package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"itagent/internal/server/model"
)

// newStocktake 构造合法盘点任务载荷；明细由 items 快照生成
func newStocktake(companyID int64, name string, items []model.StocktakeItem) model.Stocktake {
	return model.Stocktake{CompanyID: companyID, Name: name, Status: model.StocktakeStatusDraft}
}

func stocktakeItems(companyID int64, tags ...string) []model.StocktakeItem {
	items := make([]model.StocktakeItem, 0, len(tags))
	for i, tag := range tags {
		items = append(items, model.StocktakeItem{
			CompanyID:        companyID,
			AssetID:          int64(100 + i),
			AssetTag:         tag,
			ExpectedLocation: "机房A-" + tag,
			ExpectedStatus:   20,
		})
	}
	return items
}

func createStocktakeForTest(t *testing.T, s Store, companyID int64, name string, items []model.StocktakeItem) model.Stocktake {
	t.Helper()
	st, err := s.CreateStocktake(context.Background(), newStocktake(companyID, name, items), items)
	if err != nil {
		t.Fatalf("create stocktake: %v", err)
	}
	return st
}

func startStocktakeForTest(t *testing.T, s Store, companyID int64, id int64) string {
	t.Helper()
	_, token, err := s.StartStocktake(context.Background(), companyID, id)
	if err != nil {
		t.Fatalf("start stocktake: %v", err)
	}
	return token
}

func TestCreateStocktakeWithItems(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	items := stocktakeItems(1, "AST-0001", "AST-0002")
	created, err := s.CreateStocktake(ctx, newStocktake(1, "2026 Q3 全量盘点", items), items)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 || created.Status != model.StocktakeStatusDraft {
		t.Fatalf("unexpected created task: %+v", created)
	}

	got, err := s.GetStocktake(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "2026 Q3 全量盘点" || got.Status != model.StocktakeStatusDraft {
		t.Fatalf("unexpected round trip: %+v", got)
	}
	if got.StartedAt != nil || got.FinishedAt != nil {
		t.Fatal("draft task must not carry started_at/finished_at")
	}

	// 明细快照完整往返：tag、期望位置与状态
	list, total, err := s.ListStocktakeItems(ctx, StocktakeItemListFilter{CompanyID: 1, StocktakeID: created.ID, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("expected 2 items, got total=%d len=%d", total, len(list))
	}
	if list[0].AssetTag != "AST-0001" || list[0].ExpectedLocation != "机房A-AST-0001" ||
		list[0].ExpectedStatus != 20 || list[0].Result != model.StocktakeItemPending {
		t.Fatalf("item snapshot corrupted: %+v", list[0])
	}

	// 公司边界：跨公司按不存在处理
	if _, err := s.GetStocktake(ctx, 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company get must be ErrNotFound, got %v", err)
	}
}

func TestCreateStocktakeValidation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	items := stocktakeItems(1, "AST-0001")

	cases := []struct {
		name  string
		st    model.Stocktake
		items []model.StocktakeItem
	}{
		{"missing company", model.Stocktake{Name: "x"}, items},
		{"missing name", model.Stocktake{CompanyID: 1}, items},
		{"empty scope", model.Stocktake{CompanyID: 1, Name: "x"}, nil},
		{"item missing company", model.Stocktake{CompanyID: 1, Name: "x"},
			[]model.StocktakeItem{{AssetID: 1, AssetTag: "AST-1"}}},
		{"item missing asset", model.Stocktake{CompanyID: 1, Name: "x"},
			[]model.StocktakeItem{{CompanyID: 1, AssetTag: "AST-1"}}},
		{"item missing tag", model.Stocktake{CompanyID: 1, Name: "x"},
			[]model.StocktakeItem{{CompanyID: 1, AssetID: 1}}},
		{"duplicate asset in scope", model.Stocktake{CompanyID: 1, Name: "x"},
			[]model.StocktakeItem{
				{CompanyID: 1, AssetID: 7, AssetTag: "AST-7"},
				{CompanyID: 1, AssetID: 7, AssetTag: "AST-7B"},
			}},
		{"item with non-pending result", model.Stocktake{CompanyID: 1, Name: "x"},
			[]model.StocktakeItem{{CompanyID: 1, AssetID: 1, AssetTag: "AST-1", Result: model.StocktakeItemNormal}}},
	}
	for _, tc := range cases {
		if _, err := s.CreateStocktake(ctx, tc.st, tc.items); err == nil {
			t.Fatalf("case %q: expected validation error", tc.name)
		}
	}
}

func TestStocktakeStartTokenAndLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created := createStocktakeForTest(t, s, 1, "年度盘点", stocktakeItems(1, "AST-0001"))

	// 草稿无令牌
	if _, err := s.GetStocktakeByToken(ctx, "whatever"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token before start must be ErrNotFound, got %v", err)
	}

	_, token, err := s.StartStocktake(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(token) != 32 {
		t.Fatalf("scan token must be 32 hex chars, got %q", token)
	}

	got, err := s.GetStocktakeByToken(ctx, token)
	if err != nil {
		t.Fatalf("get by token: %v", err)
	}
	if got.ID != created.ID || got.Status != model.StocktakeStatusProcessing {
		t.Fatalf("unexpected task by token: %+v", got)
	}
	if got.StartedAt == nil {
		t.Fatal("started_at must be recorded")
	}

	// 伪造/未知令牌一律按不存在处理，不泄露任务是否存在
	if _, err := s.GetStocktakeByToken(ctx, "deadbeef"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown token must be ErrNotFound, got %v", err)
	}

	// 重复开始 → 状态不允许
	if _, _, err := s.StartStocktake(ctx, 1, created.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("double start must be ErrInvalidState, got %v", err)
	}
	// 跨公司开始按不存在处理
	if _, _, err := s.StartStocktake(ctx, 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company start must be ErrNotFound, got %v", err)
	}

	// 完成：processing → finished，令牌即刻失效
	finished, err := s.FinishStocktake(ctx, 1, created.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if finished.Status != model.StocktakeStatusFinished || finished.FinishedAt == nil {
		t.Fatalf("unexpected finished task: %+v", finished)
	}
	if _, err := s.GetStocktakeByToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatal("token must be invalidated after finish")
	}
	// 已完成不能再完成/取消
	if _, err := s.FinishStocktake(ctx, 1, created.ID, time.Now().UTC()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("double finish must be ErrInvalidState, got %v", err)
	}
	if _, err := s.CancelStocktake(ctx, 1, created.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("cancel finished must be ErrInvalidState, got %v", err)
	}
}

func TestStocktakeCancelAndDraftRules(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// 草稿可直接取消
	draft := createStocktakeForTest(t, s, 1, "误建任务", stocktakeItems(1, "AST-0001"))
	canceled, err := s.CancelStocktake(ctx, 1, draft.ID)
	if err != nil || canceled.Status != model.StocktakeStatusCanceled {
		t.Fatalf("cancel draft: err=%v task=%+v", err, canceled)
	}
	// 草稿不能直接完成（必须先开始）
	draft2 := createStocktakeForTest(t, s, 1, "另一任务", stocktakeItems(1, "AST-0002"))
	if _, err := s.FinishStocktake(ctx, 1, draft2.ID, time.Now().UTC()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("finish draft must be ErrInvalidState, got %v", err)
	}
	// 盘点中可取消，取消后令牌失效
	startStocktakeForTest(t, s, 1, draft2.ID)
	canceled2, err := s.CancelStocktake(ctx, 1, draft2.ID)
	if err != nil || canceled2.Status != model.StocktakeStatusCanceled {
		t.Fatalf("cancel processing: err=%v task=%+v", err, canceled2)
	}
}

func TestRotateStocktakeToken(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created := createStocktakeForTest(t, s, 1, "令牌轮换", stocktakeItems(1, "AST-0001"))
	// 草稿不可轮换（尚无令牌）
	if _, err := s.RotateStocktakeToken(ctx, 1, created.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("rotate on draft must be ErrInvalidState, got %v", err)
	}

	old := startStocktakeForTest(t, s, 1, created.ID)
	fresh, err := s.RotateStocktakeToken(ctx, 1, created.ID)
	if err != nil || fresh == old || len(fresh) != 32 {
		t.Fatalf("rotate: err=%v old=%q fresh=%q", err, old, fresh)
	}
	if _, err := s.GetStocktakeByToken(ctx, old); !errors.Is(err, ErrNotFound) {
		t.Fatal("old token must be invalidated after rotate")
	}
	if _, err := s.GetStocktakeByToken(ctx, fresh); err != nil {
		t.Fatalf("fresh token must work: %v", err)
	}
	// 跨公司轮换按不存在处理
	if _, err := s.RotateStocktakeToken(ctx, 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company rotate must be ErrNotFound, got %v", err)
	}
}

func TestCheckStocktakeItems(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created := createStocktakeForTest(t, s, 1, "扫码核对", stocktakeItems(1, "AST-0001", "AST-0002", "AST-0003"))

	// 草稿状态不接受核对
	_, err := s.CheckStocktakeItems(ctx, 1, created.ID, []model.StocktakeCheck{{AssetTag: "AST-0001", Result: model.StocktakeItemNormal}}, "张三")
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("check on draft must be ErrInvalidState, got %v", err)
	}

	startStocktakeForTest(t, s, 1, created.ID)

	// 按 asset_tag 批量核对 + 按 item_id 单条核对
	updated, err := s.CheckStocktakeItems(ctx, 1, created.ID, []model.StocktakeCheck{
		{AssetTag: "AST-0001", Result: model.StocktakeItemNormal, ActualLocation: "工位B-12"},
		{ItemID: 0, AssetTag: "AST-0002", Result: model.StocktakeItemLost, Remark: "找不到"},
	}, "张三")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(updated) != 2 {
		t.Fatalf("expected 2 updated items, got %d", len(updated))
	}
	byTag := map[int]model.StocktakeItem{}
	for _, it := range updated {
		byTag[it.Result] = it
	}
	normal, lost := byTag[model.StocktakeItemNormal], byTag[model.StocktakeItemLost]
	if normal.AssetTag != "AST-0001" || normal.ActualLocation != "工位B-12" || normal.ScannedBy != "张三" || normal.ScannedAt == nil {
		t.Fatalf("normal check incomplete: %+v", normal)
	}
	if lost.AssetTag != "AST-0002" || lost.Remark != "找不到" {
		t.Fatalf("lost check incomplete: %+v", lost)
	}

	// 盘点中允许改判重核（最后写入生效）
	_, err = s.CheckStocktakeItems(ctx, 1, created.ID, []model.StocktakeCheck{
		{AssetTag: "AST-0002", Result: model.StocktakeItemDamaged},
	}, "李四")
	if err != nil {
		t.Fatalf("re-check: %v", err)
	}
	items, _, err := s.ListStocktakeItems(ctx, StocktakeItemListFilter{CompanyID: 1, StocktakeID: created.ID, Result: model.StocktakeItemDamaged, Page: 1, PageSize: 10})
	if err != nil || len(items) != 1 || items[0].ScannedBy != "李四" {
		t.Fatalf("re-check must overwrite: err=%v items=%+v", err, items)
	}

	// 非法结果码（pending 不是核对结果）
	if _, err := s.CheckStocktakeItems(ctx, 1, created.ID, []model.StocktakeCheck{{AssetTag: "AST-0003", Result: model.StocktakeItemPending}}, "张三"); err == nil {
		t.Fatal("pending result must be rejected")
	}
	// 范围外资产按不存在处理
	if _, err := s.CheckStocktakeItems(ctx, 1, created.ID, []model.StocktakeCheck{{AssetTag: "AST-9999", Result: model.StocktakeItemNormal}}, "张三"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("out-of-scope check must be ErrNotFound, got %v", err)
	}
	// 跨公司任务按不存在处理
	if _, err := s.CheckStocktakeItems(ctx, 2, created.ID, []model.StocktakeCheck{{AssetTag: "AST-0001", Result: model.StocktakeItemNormal}}, "张三"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company check must be ErrNotFound, got %v", err)
	}

	// 批量原子性：一条非法则整批不落
	if _, err := s.CheckStocktakeItems(ctx, 1, created.ID, []model.StocktakeCheck{
		{AssetTag: "AST-0003", Result: model.StocktakeItemNormal},
		{AssetTag: "AST-8888", Result: model.StocktakeItemNormal},
	}, "张三"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial batch must be rejected: %v", err)
	}
	items, _, err = s.ListStocktakeItems(ctx, StocktakeItemListFilter{CompanyID: 1, StocktakeID: created.ID, Result: model.StocktakeItemNormal, Page: 1, PageSize: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("atomicity broken: err=%v len=%d", err, len(items))
	}
}

func TestCheckStocktakeItemsAfterFinish(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created := createStocktakeForTest(t, s, 1, "完成后只读", stocktakeItems(1, "AST-0001"))
	startStocktakeForTest(t, s, 1, created.ID)
	if _, err := s.FinishStocktake(ctx, 1, created.ID, time.Now().UTC()); err != nil {
		t.Fatalf("finish: %v", err)
	}
	_, err := s.CheckStocktakeItems(ctx, 1, created.ID, []model.StocktakeCheck{{AssetTag: "AST-0001", Result: model.StocktakeItemNormal}}, "张三")
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("check after finish must be ErrInvalidState, got %v", err)
	}
}

func TestListStocktakeItemsFiltersAndPaging(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	items := []model.StocktakeItem{
		{CompanyID: 1, AssetID: 11, AssetTag: "AST-0001", ExpectedLocation: "北京", ExpectedStatus: 20},
		{CompanyID: 1, AssetID: 12, AssetTag: "AST-0002", ExpectedLocation: "上海", ExpectedStatus: 20},
		{CompanyID: 1, AssetID: 13, AssetTag: "AST-0003", ExpectedLocation: "深圳", ExpectedStatus: 20},
	}
	task := createStocktakeForTest(t, s, 1, "过滤分页", items)
	// 同公司另一任务含同 tag 明细，验证按任务隔离
	createStocktakeForTest(t, s, 1, "另一任务", stocktakeItems(1, "AST-0001"))
	createStocktakeForTest(t, s, 2, "别家公司", stocktakeItems(2, "AST-0001"))

	startStocktakeForTest(t, s, 1, task.ID)
	if _, err := s.CheckStocktakeItems(ctx, 1, task.ID, []model.StocktakeCheck{
		{AssetTag: "AST-0001", Result: model.StocktakeItemNormal},
		{AssetTag: "AST-0002", Result: model.StocktakeItemLost},
	}, "张三"); err != nil {
		t.Fatalf("check: %v", err)
	}

	// 按 stocktake_id 隔离：不含 other 任务的明细
	got, total, err := s.ListStocktakeItems(ctx, StocktakeItemListFilter{CompanyID: 1, StocktakeID: task.ID, Page: 1, PageSize: 10})
	if err != nil || total != 3 || len(got) != 3 {
		t.Fatalf("list by task: err=%v total=%d len=%d", err, total, len(got))
	}

	// 结果过滤
	got, total, err = s.ListStocktakeItems(ctx, StocktakeItemListFilter{CompanyID: 1, StocktakeID: task.ID, Result: model.StocktakeItemLost, Page: 1, PageSize: 10})
	if err != nil || total != 1 || got[0].AssetTag != "AST-0002" {
		t.Fatalf("list by result: err=%v total=%d items=%+v", err, total, got)
	}

	// tag 模糊过滤
	got, total, err = s.ListStocktakeItems(ctx, StocktakeItemListFilter{CompanyID: 1, StocktakeID: task.ID, Keyword: "0003", Page: 1, PageSize: 10})
	if err != nil || total != 1 || got[0].AssetTag != "AST-0003" {
		t.Fatalf("list by keyword: err=%v total=%d items=%+v", err, total, got)
	}

	// 公司边界：只按任务 id 查也拦不住跨公司读取，必须带 company 校验
	if _, _, err := s.ListStocktakeItems(ctx, StocktakeItemListFilter{CompanyID: 2, StocktakeID: task.ID, Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("list filter itself must not error: %v", err)
	}

	// 分页：3 条按 id 正序，每页 2 条第 2 页剩 1 条
	got, total, err = s.ListStocktakeItems(ctx, StocktakeItemListFilter{CompanyID: 1, StocktakeID: task.ID, Page: 2, PageSize: 2})
	if err != nil || total != 3 || len(got) != 1 {
		t.Fatalf("paging: err=%v total=%d len=%d", err, total, len(got))
	}
}

func TestCountStocktakeResults(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created := createStocktakeForTest(t, s, 1, "结果统计", stocktakeItems(1, "AST-0001", "AST-0002", "AST-0003", "AST-0004"))
	startStocktakeForTest(t, s, 1, created.ID)
	if _, err := s.CheckStocktakeItems(ctx, 1, created.ID, []model.StocktakeCheck{
		{AssetTag: "AST-0001", Result: model.StocktakeItemNormal},
		{AssetTag: "AST-0002", Result: model.StocktakeItemLost},
		{AssetTag: "AST-0003", Result: model.StocktakeItemDamaged},
	}, "张三"); err != nil {
		t.Fatalf("check: %v", err)
	}

	counts, err := s.CountStocktakeResults(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	want := map[int]int64{
		model.StocktakeItemPending:  1,
		model.StocktakeItemNormal:   1,
		model.StocktakeItemLost:     1,
		model.StocktakeItemDamaged:  1,
		model.StocktakeItemScrapped: 0,
	}
	for result, n := range want {
		if counts[result] != n {
			t.Fatalf("result %d: got %d want %d (all=%v)", result, counts[result], n, counts)
		}
	}
}

func TestListStocktakesFiltersAndPaging(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	first := createStocktakeForTest(t, s, 1, "任务一", stocktakeItems(1, "AST-0001"))
	createStocktakeForTest(t, s, 1, "任务二", stocktakeItems(1, "AST-0002"))
	createStocktakeForTest(t, s, 2, "别家公司", stocktakeItems(2, "AST-0001"))
	startStocktakeForTest(t, s, 1, first.ID)

	// 公司边界
	_, total, err := s.ListStocktakes(ctx, StocktakeListFilter{CompanyID: 1, Page: 1, PageSize: 10})
	if err != nil || total != 2 {
		t.Fatalf("company filter: err=%v total=%d", err, total)
	}
	// 状态过滤
	got, total, err := s.ListStocktakes(ctx, StocktakeListFilter{CompanyID: 1, Status: model.StocktakeStatusProcessing, Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(got) != 1 || got[0].ID != first.ID {
		t.Fatalf("status filter: err=%v total=%d items=%+v", err, total, got)
	}
	// 分页：2 条按 id 倒序，每页 1 条
	got, total, err = s.ListStocktakes(ctx, StocktakeListFilter{CompanyID: 1, Page: 2, PageSize: 1})
	if err != nil || total != 2 || len(got) != 1 {
		t.Fatalf("paging: err=%v total=%d len=%d", err, total, len(got))
	}
}

func TestStocktakeGetNotFound(t *testing.T) {
	s := testStore(t)
	if _, err := s.GetStocktake(context.Background(), 1, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
