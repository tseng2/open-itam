package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"itagent/internal/server/model"
)

// P2 操作日志：store 契约测试（SQLiteStore 实现）。
// 契约要点：追加式落库全字段 round-trip；CompanyID 0 = 不过滤（含全局）；
// 操作人/动作/对象/时间窗多条件过滤；keyword 模糊匹配操作人；分页倒序

func newOpLog(companyID, userID int64, username, action, resource, resourceID string, at time.Time) model.OperationLog {
	return model.OperationLog{
		CompanyID:  companyID,
		UserID:     userID,
		Username:   username,
		Role:       "admin",
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Path:       "/api/v1/" + resource,
		Detail:     `{"company_id":` + resourceID + `}`,
		IP:         "10.1.1.1",
		UserAgent:  "Mozilla/5.0",
		Status:     200,
		CreatedAt:  at,
	}
}

func TestOperationLogCreateRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	before := time.Now().UTC().Add(-time.Minute)
	log := newOpLog(1, 42, "张三", model.OperationActionCreate, "assets", "101", before)
	log.Status = 409 // 失败尝试也要如实留痕
	if err := s.CreateOperationLog(ctx, log); err != nil {
		t.Fatalf("create: %v", err)
	}

	items, total, err := s.ListOperationLogs(ctx, OperationLogListFilter{Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("list: total=%d items=%v err=%v", total, items, err)
	}
	got := items[0]
	if got.ID == 0 || got.Username != "张三" || got.Role != "admin" ||
		got.Action != model.OperationActionCreate || got.Resource != "assets" ||
		got.ResourceID != "101" || got.Status != 409 || got.IP != "10.1.1.1" ||
		got.UserAgent != "Mozilla/5.0" || got.Path != "/api/v1/assets" || got.Detail == "" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("created_at must be persisted")
	}

	// 全局操作（company_id=0）与登录失败事件同样可落库
	if err := s.CreateOperationLog(ctx, newOpLog(0, 0, "admin", model.OperationActionLoginFailed, "auth", "", before)); err != nil {
		t.Fatalf("create global login event: %v", err)
	}
	_, total, _ = s.ListOperationLogs(ctx, OperationLogListFilter{Page: 1, PageSize: 10})
	if total != 2 {
		t.Fatalf("global log must be stored, total=%d", total)
	}
}

func TestOperationLogListFilters(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	day := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	logs := []model.OperationLog{
		newOpLog(1, 1, "张三", model.OperationActionCreate, "assets", "11", day),
		newOpLog(1, 2, "李四", model.OperationActionUpdate, "assets", "11", day.Add(time.Hour)),
		newOpLog(1, 1, "张三", model.OperationActionDelete, "manufacturers", "3", day.Add(2*time.Hour)),
		newOpLog(2, 3, "王五", model.OperationActionCreate, "stocktakes", "7", day.Add(3*time.Hour)),
		newOpLog(0, 1, "张三", model.OperationActionLogin, "auth", "", day.Add(4*time.Hour)),
	}
	for _, l := range logs {
		if err := s.CreateOperationLog(ctx, l); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	f := func(mod func(*OperationLogListFilter)) ([]model.OperationLog, int64) {
		t.Helper()
		base := OperationLogListFilter{Page: 1, PageSize: 50}
		mod(&base)
		items, total, err := s.ListOperationLogs(ctx, base)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return items, total
	}

	// 不过滤 = 全部（含全局）
	if _, total := f(func(x *OperationLogListFilter) {}); total != 5 {
		t.Fatalf("no filter: total=%d", total)
	}
	// 公司过滤：全局记录不混入公司视图
	if _, total := f(func(x *OperationLogListFilter) { x.CompanyID = 1 }); total != 3 {
		t.Fatalf("company filter: total=%d", total)
	}
	// 操作人 ID 过滤
	if items, total := f(func(x *OperationLogListFilter) { x.CompanyID = 1; x.UserID = 1 }); total != 2 || items[0].Resource != "manufacturers" {
		t.Fatalf("user filter: total=%d first=%+v", total, items[0])
	}
	// keyword 模糊匹配操作人
	if _, total := f(func(x *OperationLogListFilter) { x.Keyword = "王" }); total != 1 {
		t.Fatalf("keyword filter: total=%d", total)
	}
	// 动作 + 对象 + 对象 ID 组合过滤
	if _, total := f(func(x *OperationLogListFilter) {
		x.Action = model.OperationActionCreate
		x.Resource = "assets"
		x.ResourceID = "11"
	}); total != 1 {
		t.Fatalf("action+resource filter: total=%d", total)
	}
	// 时间窗：含边界
	start := day.Add(time.Hour)
	end := day.Add(3 * time.Hour)
	if _, total := f(func(x *OperationLogListFilter) { x.StartTime = &start; x.EndTime = &end }); total != 3 {
		t.Fatalf("time window: total=%d", total)
	}
	// 时间窗外
	if _, total := f(func(x *OperationLogListFilter) { x.StartTime = &end }); total != 2 {
		t.Fatalf("after window: total=%d", total)
	}
}

func TestOperationLogPaginationOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		l := newOpLog(1, int64(i+1), "操作员", model.OperationActionUpdate, "assets", "1", time.Now().UTC())
		if err := s.CreateOperationLog(ctx, l); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// 倒序：最新（后插入）在前；分页 total 全量
	items, total, err := s.ListOperationLogs(ctx, OperationLogListFilter{CompanyID: 1, Page: 2, PageSize: 2})
	if err != nil || total != 5 || len(items) != 2 {
		t.Fatalf("pagination: total=%d len=%d err=%v", total, len(items), err)
	}
	if items[0].UserID < items[1].UserID {
		t.Fatalf("must be id desc, got %d then %d", items[0].UserID, items[1].UserID)
	}
	first, _, _ := s.ListOperationLogs(ctx, OperationLogListFilter{Page: 1, PageSize: 2})
	if first[0].UserID != 5 {
		t.Fatalf("newest first: %+v", first[0])
	}

	// 分页缺省与上限钳制：Page 0 → 1、PageSize 0 → 20、超限 → 200
	if _, total, _ = s.ListOperationLogs(ctx, OperationLogListFilter{}); total != 5 {
		t.Fatalf("default paging: total=%d", total)
	}
	page, size := normalizeOperationLogPage(0, 0)
	if page != 1 || size != 20 {
		t.Fatalf("paging defaults: page=%d size=%d", page, size)
	}
	if page, size = normalizeOperationLogPage(-3, 999); page != 1 || size != 200 {
		t.Fatalf("paging clamp: page=%d size=%d", page, size)
	}
}

func TestOperationLogColumnWidthGuard(t *testing.T) {
	s := testStore(t)
	// 列宽防御：审计是旁路，超宽字段截断落库而非拒写（MariaDB 严格模式
	// 会因超宽丢弃整条审计）
	l := model.OperationLog{
		Action:     model.OperationActionLoginFailed,
		Resource:   "auth",
		Username:   strings.Repeat("名", 200),
		UserAgent:  strings.Repeat("ua", 300),
		Path:       strings.Repeat("/p", 200),
	}
	if err := s.CreateOperationLog(context.Background(), l); err != nil {
		t.Fatalf("oversized columns must not be rejected: %v", err)
	}
	items, _, _ := s.ListOperationLogs(context.Background(), OperationLogListFilter{Page: 1, PageSize: 10})
	if len(items) != 1 {
		t.Fatalf("row must persist, got %d", len(items))
	}
	if len(items[0].UserAgent) != 255 || len(items[0].Path) != 255 {
		t.Fatalf("columns must clamp to width: ua=%d path=%d",
			len(items[0].UserAgent), len(items[0].Path))
	}
	// 必填校验：动作/对象缺失直接拒绝（防无意义审计行）
	if err := s.CreateOperationLog(context.Background(), model.OperationLog{Resource: "x"}); err == nil {
		t.Fatal("missing action must fail")
	}
	if err := s.CreateOperationLog(context.Background(), model.OperationLog{Action: "create"}); err == nil {
		t.Fatal("missing resource must fail")
	}
}
