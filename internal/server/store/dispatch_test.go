package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"itagent/internal/server/model"
)

func newDispatch(companyID, assetID int64, borrower, destination string, expectedReturn time.Time) model.AssetDispatch {
	return model.AssetDispatch{
		CompanyID:        companyID,
		AssetID:          assetID,
		BorrowerName:     borrower,
		Destination:      destination,
		ExpectedReturnAt: expectedReturn,
	}
}

func TestCreateDispatchSuccess(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	d := newDispatch(1, 101, "张三", "深圳客户现场", time.Now().UTC().Add(72*time.Hour))
	d.IsolationOffline = true
	d.ExpectWipe = true
	created, err := s.CreateDispatch(ctx, d)
	if err != nil {
		t.Fatalf("create dispatch: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero id")
	}
	if created.Status != model.DispatchStatusActive {
		t.Fatalf("expected status %d, got %d", model.DispatchStatusActive, created.Status)
	}
	if created.DispatchedAt.IsZero() {
		t.Fatal("dispatched_at must default to now")
	}

	got, err := s.GetActiveDispatchByAsset(ctx, 1, 101)
	if err != nil {
		t.Fatalf("get active dispatch: %v", err)
	}
	if got.ID != created.ID || got.BorrowerName != "张三" || got.Destination != "深圳客户现场" {
		t.Fatalf("unexpected record: %+v", got)
	}
	if !got.ExpectedReturnAt.Equal(created.ExpectedReturnAt) {
		t.Fatalf("expected_return_at not preserved: %v != %v", got.ExpectedReturnAt, created.ExpectedReturnAt)
	}
	// 隔离与格式化标记是 A2 失联分层/涉密归还的业务依据，必须完整往返
	if !got.IsolationOffline || !got.ExpectWipe {
		t.Fatalf("isolation flags not preserved: offline=%v wipe=%v", got.IsolationOffline, got.ExpectWipe)
	}
}

func TestCreateDispatchRejectsMissingFields(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	future := time.Now().UTC().Add(48 * time.Hour)
	cases := []model.AssetDispatch{
		newDispatch(0, 101, "张三", "深圳", future),
		newDispatch(1, 0, "张三", "深圳", future),
		newDispatch(1, 101, "", "深圳", future),
		newDispatch(1, 101, "张三", "深圳", time.Time{}),
	}
	for i, d := range cases {
		if _, err := s.CreateDispatch(ctx, d); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestCreateDispatchRejectsNonActiveStatus(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	d := newDispatch(1, 101, "张三", "深圳", time.Now().UTC().Add(48*time.Hour))
	d.Status = model.DispatchStatusReturned
	if _, err := s.CreateDispatch(ctx, d); err == nil {
		t.Fatal("create with non-active status must be rejected")
	}
}

func TestCreateDispatchRejectsSecondActiveButAllowsAfterClosed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	first, err := s.CreateDispatch(ctx, newDispatch(1, 101, "张三", "深圳", time.Now().UTC().Add(72*time.Hour)))
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := s.CreateDispatch(ctx, newDispatch(1, 101, "李四", "上海", time.Now().UTC().Add(48*time.Hour))); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second active dispatch must be rejected with ErrAlreadyExists, got %v", err)
	}

	// 归还后允许同一资产再次外派
	if _, err := s.ReturnDispatch(ctx, 1, first.ID, time.Now().UTC()); err != nil {
		t.Fatalf("return first: %v", err)
	}
	second, err := s.CreateDispatch(ctx, newDispatch(1, 101, "李四", "上海", time.Now().UTC().Add(48*time.Hour)))
	if err != nil {
		t.Fatalf("create after return: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected a new record after return")
	}

	// 作废后同样允许再次外派
	if _, err := s.CancelDispatch(ctx, 1, second.ID); err != nil {
		t.Fatalf("cancel second: %v", err)
	}
	if _, err := s.CreateDispatch(ctx, newDispatch(1, 101, "王五", "北京", time.Now().UTC().Add(48*time.Hour))); err != nil {
		t.Fatalf("create after cancel: %v", err)
	}
}

func TestListDispatchesFiltersAndPaging(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-24 * time.Hour)
	future := time.Now().UTC().Add(72 * time.Hour)

	mustCreate := func(companyID, assetID int64, borrower, dest string, expected time.Time) model.AssetDispatch {
		t.Helper()
		created, err := s.CreateDispatch(ctx, newDispatch(companyID, assetID, borrower, dest, expected))
		if err != nil {
			t.Fatalf("create dispatch: %v", err)
		}
		return created
	}

	overdue := mustCreate(1, 101, "张三", "深圳", past) // 外派中且已过预计归期
	onTrip := mustCreate(1, 102, "李四", "上海", future) // 外派中未超期
	returned := mustCreate(1, 103, "王五", "北京", future)
	if _, err := s.ReturnDispatch(ctx, 1, returned.ID, time.Now().UTC()); err != nil {
		t.Fatalf("return: %v", err)
	}
	mustCreate(2, 201, "赵六", "广州", future) // 另一家公司，验证租户隔离

	// 公司边界过滤
	_, total, err := s.ListDispatches(ctx, DispatchListFilter{CompanyID: 1, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list company 1: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 records for company 1, got %d", total)
	}

	// 状态过滤：仅外派中
	items, total, err := s.ListDispatches(ctx, DispatchListFilter{CompanyID: 1, Status: model.DispatchStatusActive, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("expected 2 active records, got total=%d len=%d", total, len(items))
	}

	// 资产过滤
	items, total, err = s.ListDispatches(ctx, DispatchListFilter{CompanyID: 1, AssetID: 102, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list by asset: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != onTrip.ID {
		t.Fatalf("expected onTrip record, got total=%d items=%+v", total, items)
	}

	// 超期过滤：true 仅超期未归
	overdueOnly := true
	items, total, err = s.ListDispatches(ctx, DispatchListFilter{CompanyID: 1, Overdue: &overdueOnly, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list overdue: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != overdue.ID {
		t.Fatalf("expected overdue record, got total=%d items=%+v", total, items)
	}

	// 超期过滤：false 排除超期未归
	notOverdue := false
	_, total, err = s.ListDispatches(ctx, DispatchListFilter{CompanyID: 1, Overdue: &notOverdue, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list not overdue: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 non-overdue records, got %d", total)
	}

	// 分页：3 条记录按 id 倒序，第 2 页每页 2 条应剩 1 条
	items, total, err = s.ListDispatches(ctx, DispatchListFilter{CompanyID: 1, Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("list paged: %v", err)
	}
	if total != 3 || len(items) != 1 {
		t.Fatalf("expected 1 item on page 2, got total=%d len=%d", total, len(items))
	}
}

func TestReturnDispatchLifecycleAndBoundary(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateDispatch(ctx, newDispatch(1, 101, "张三", "深圳", time.Now().UTC().Add(48*time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 允许补录实际归还时间
	returnedAt := time.Now().UTC().Add(-time.Hour)
	got, err := s.ReturnDispatch(ctx, 1, created.ID, returnedAt)
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	if got.Status != model.DispatchStatusReturned {
		t.Fatalf("expected status returned, got %d", got.Status)
	}
	if got.ReturnedAt == nil || got.ReturnedAt.Unix() != returnedAt.Unix() {
		t.Fatalf("returned_at not recorded: %+v", got.ReturnedAt)
	}

	// 已归还记录不能再次归还
	if _, err := s.ReturnDispatch(ctx, 1, created.ID, time.Now().UTC()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("double return must fail with ErrInvalidState, got %v", err)
	}
	// 不存在的记录
	if _, err := s.ReturnDispatch(ctx, 1, 99999, time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	// 跨公司边界按不存在处理，防止水平越权
	if _, err := s.ReturnDispatch(ctx, 2, created.ID, time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company return must be ErrNotFound, got %v", err)
	}
	// 归还后不存在进行中的外派
	if _, err := s.GetActiveDispatchByAsset(ctx, 1, 101); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after return, got %v", err)
	}
}

func TestCancelDispatchLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateDispatch(ctx, newDispatch(1, 101, "张三", "深圳", time.Now().UTC().Add(48*time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 跨公司作废按不存在处理
	if _, err := s.CancelDispatch(ctx, 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company cancel must be ErrNotFound, got %v", err)
	}

	got, err := s.CancelDispatch(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got.Status != model.DispatchStatusCanceled {
		t.Fatalf("expected status canceled, got %d", got.Status)
	}
	if got.ReturnedAt != nil {
		t.Fatal("cancel must not set returned_at")
	}

	// 已作废不能归还，也不能再次作废
	if _, err := s.ReturnDispatch(ctx, 1, created.ID, time.Now().UTC()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("return canceled must fail with ErrInvalidState, got %v", err)
	}
	if _, err := s.CancelDispatch(ctx, 1, created.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("double cancel must fail with ErrInvalidState, got %v", err)
	}
}

func TestGetActiveDispatchByAssetBoundary(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateDispatch(ctx, newDispatch(1, 101, "张三", "深圳", time.Now().UTC().Add(48*time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetActiveDispatchByAsset(ctx, 1, 101)
	if err != nil || got.ID != created.ID {
		t.Fatalf("get active: err=%v record=%+v", err, got)
	}
	if _, err := s.GetActiveDispatchByAsset(ctx, 2, 101); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company lookup must be ErrNotFound, got %v", err)
	}
	if _, err := s.GetActiveDispatchByAsset(ctx, 1, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown asset must be ErrNotFound, got %v", err)
	}
}

func TestNormalizeDispatchPageGuards(t *testing.T) {
	cases := []struct {
		page, pageSize, wantPage, wantSize int
	}{
		{0, 0, 1, 20},          // 缺省兜底
		{-3, -1, 1, 20},        // 负值兜底
		{2, 50, 2, 50},         // 合法值透传
		{1, 1000, 1, 100},      // 超大页封顶，防止全表拉取
	}
	for _, tc := range cases {
		page, size := normalizeDispatchPage(tc.page, tc.pageSize)
		if page != tc.wantPage || size != tc.wantSize {
			t.Fatalf("normalize(%d,%d) = (%d,%d), want (%d,%d)",
				tc.page, tc.pageSize, page, size, tc.wantPage, tc.wantSize)
		}
	}
}
