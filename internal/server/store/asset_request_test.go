package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"itagent/internal/server/model"
)

func newAssetRequest(companyID, assetID, applicantID int64, name, reason string, longTerm bool, returnAt *time.Time) model.AssetRequest {
	return model.AssetRequest{
		CompanyID:        companyID,
		AssetID:           assetID,
		ApplicantID:       applicantID,
		ApplicantName:     name,
		Reason:            reason,
		IsLongTerm:        longTerm,
		ExpectedReturnAt:  returnAt,
	}
}

func TestCreateAssetRequest(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// 长期领用：即使误传归期也归一清空
	leakedReturn := time.Now().UTC().Add(720 * time.Hour)
	created, err := s.CreateAssetRequest(ctx, newAssetRequest(1, 101, 11, "张三", "新项目长期开发用机", true, &leakedReturn))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 || created.Status != model.AssetRequestStatusPending {
		t.Fatalf("unexpected created: %+v", created)
	}
	if created.ExpectedReturnAt != nil {
		t.Fatal("long-term request must not carry expected_return_at")
	}

	// 短期借用：归期必须完整往返（审批通过后会写入履历 ReturnDate）
	ret := time.Now().UTC().Add(72 * time.Hour)
	created, err = s.CreateAssetRequest(ctx, newAssetRequest(1, 102, 11, "张三", "外勤备用机", false, &ret))
	if err != nil {
		t.Fatalf("create short-term: %v", err)
	}
	got, err := s.GetAssetRequest(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ApplicantName != "张三" || got.Reason != "外勤备用机" {
		t.Fatalf("snapshot corrupted: %+v", got)
	}
	if got.ExpectedReturnAt == nil || !got.ExpectedReturnAt.Equal(ret) {
		t.Fatalf("expected_return_at not preserved: %+v", got.ExpectedReturnAt)
	}

	// 公司边界：跨公司按不存在处理
	if _, err := s.GetAssetRequest(ctx, 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company get must be ErrNotFound, got %v", err)
	}
}

func TestCreateAssetRequestValidation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ret := time.Now().UTC().Add(48 * time.Hour)

	cases := []model.AssetRequest{
		newAssetRequest(0, 101, 11, "张三", "事由", false, &ret),
		newAssetRequest(1, 0, 11, "张三", "事由", false, &ret),
		newAssetRequest(1, 101, 0, "张三", "事由", false, &ret),
		newAssetRequest(1, 101, 11, "", "事由", false, &ret),
		newAssetRequest(1, 101, 11, "张三", "", false, &ret),
		newAssetRequest(1, 101, 11, "张三", "事由", false, nil), // 短期借用缺归期
	}
	for i, r := range cases {
		if _, err := s.CreateAssetRequest(ctx, r); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestCreateAssetRequestDuplicatePending(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ret := time.Now().UTC().Add(48 * time.Hour)

	first, err := s.CreateAssetRequest(ctx, newAssetRequest(1, 101, 11, "张三", "要用", false, &ret))
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	// 同人同资产重复挂 pending → 冲突
	if _, err := s.CreateAssetRequest(ctx, newAssetRequest(1, 101, 11, "张三", "再要一台", false, &ret)); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate pending must be ErrAlreadyExists, got %v", err)
	}
	// 不同申请人竞争同一资产 → 允许（留给审批人裁决）
	if _, err := s.CreateAssetRequest(ctx, newAssetRequest(1, 101, 12, "李四", "我也要", false, &ret)); err != nil {
		t.Fatalf("competing request must be allowed: %v", err)
	}

	// 结束（驳回/取消）后允许重新申请
	if _, err := s.RejectAssetRequest(ctx, 1, first.ID, 9, "库存不足，下季度再议", time.Now().UTC()); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if _, err := s.CreateAssetRequest(ctx, newAssetRequest(1, 101, 11, "张三", "重新申请", false, &ret)); err != nil {
		t.Fatalf("re-create after rejected: %v", err)
	}
}

func TestListAssetRequestsFiltersAndPaging(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ret := time.Now().UTC().Add(48 * time.Hour)

	mustCreate := func(companyID, assetID, applicant int64, name string) model.AssetRequest {
		t.Helper()
		r, err := s.CreateAssetRequest(ctx, newAssetRequest(companyID, assetID, applicant, name, "事由", false, &ret))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		return r
	}
	zhang := mustCreate(1, 101, 11, "张三")
	mustCreate(1, 102, 12, "李四")
	liCanceled := mustCreate(1, 103, 12, "李四")
	if _, err := s.CancelAssetRequest(ctx, 1, liCanceled.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	mustCreate(2, 201, 21, "别家公司")

	// 公司边界
	_, total, err := s.ListAssetRequests(ctx, AssetRequestListFilter{CompanyID: 1, Page: 1, PageSize: 10})
	if err != nil || total != 3 {
		t.Fatalf("company filter: err=%v total=%d", err, total)
	}
	// 状态过滤
	items, total, err := s.ListAssetRequests(ctx, AssetRequestListFilter{CompanyID: 1, Status: model.AssetRequestStatusCanceled, Page: 1, PageSize: 10})
	if err != nil || total != 1 || items[0].ID != liCanceled.ID {
		t.Fatalf("status filter: err=%v total=%d items=%+v", err, total, items)
	}
	// 申请人过滤（user 视角只看自己的）
	_, total, err = s.ListAssetRequests(ctx, AssetRequestListFilter{CompanyID: 1, ApplicantID: 11, Page: 1, PageSize: 10})
	if err != nil || total != 1 {
		t.Fatalf("applicant filter: err=%v total=%d", err, total)
	}
	// 资产过滤
	_, total, err = s.ListAssetRequests(ctx, AssetRequestListFilter{CompanyID: 1, AssetID: zhang.AssetID, Page: 1, PageSize: 10})
	if err != nil || total != 1 {
		t.Fatalf("asset filter: err=%v total=%d", err, total)
	}
	// 分页：3 条按 id 倒序，每页 2 条第 2 页剩 1 条
	items, total, err = s.ListAssetRequests(ctx, AssetRequestListFilter{CompanyID: 1, Page: 2, PageSize: 2})
	if err != nil || total != 3 || len(items) != 1 {
		t.Fatalf("paging: err=%v total=%d len=%d", err, total, len(items))
	}
}

func TestRejectAssetRequest(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateAssetRequest(ctx, newAssetRequest(1, 101, 11, "张三", "事由", true, nil))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 跨公司 → 404 语义
	if _, err := s.RejectAssetRequest(ctx, 2, created.ID, 9, "x", time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company reject must be ErrNotFound, got %v", err)
	}

	now := time.Now().UTC()
	rejected, err := s.RejectAssetRequest(ctx, 1, created.ID, 9, "库存紧张", now)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != model.AssetRequestStatusRejected || rejected.DecisionRemark != "库存紧张" {
		t.Fatalf("unexpected rejected: %+v", rejected)
	}
	if rejected.ApprovedBy == nil || *rejected.ApprovedBy != 9 || rejected.ApprovedAt == nil {
		t.Fatalf("decider not recorded: %+v", rejected)
	}

	// 已驳回不能再驳回/取消
	if _, err := s.RejectAssetRequest(ctx, 1, created.ID, 9, "x", now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("double reject must be ErrInvalidState, got %v", err)
	}
	if _, err := s.CancelAssetRequest(ctx, 1, created.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("cancel rejected must be ErrInvalidState, got %v", err)
	}
	// 不存在
	if _, err := s.RejectAssetRequest(ctx, 1, 99999, 9, "x", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCancelAssetRequest(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateAssetRequest(ctx, newAssetRequest(1, 101, 11, "张三", "事由", true, nil))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 跨公司按不存在处理
	if _, err := s.CancelAssetRequest(ctx, 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company cancel must be ErrNotFound, got %v", err)
	}

	canceled, err := s.CancelAssetRequest(ctx, 1, created.ID)
	if err != nil || canceled.Status != model.AssetRequestStatusCanceled {
		t.Fatalf("cancel: err=%v record=%+v", err, canceled)
	}
	// 双重取消 → 状态不允许
	if _, err := s.CancelAssetRequest(ctx, 1, created.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("double cancel must be ErrInvalidState, got %v", err)
	}
}

func TestGetAssetRequestNotFound(t *testing.T) {
	s := testStore(t)
	if _, err := s.GetAssetRequest(context.Background(), 1, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
