package store

import (
	"context"
	"errors"
	"testing"

	"itagent/internal/server/model"
)

func newRule(companyID int64, name string, months int, floorType string, floorVal float64, stages string) model.DepreciationRule {
	return model.DepreciationRule{
		CompanyID: companyID,
		Name:      name,
		Months:    months,
		FloorType: floorType,
		FloorVal:  floorVal,
		Stages:    stages,
		Enabled:   true,
	}
}

const ladderStagesJSON = `[{"period":12,"unit":"MONTH","ratio":0.35},{"period":12,"unit":"MONTH","ratio":0.30},{"period":12,"unit":"MONTH","ratio":0.30}]`

func TestCreateDepreciationRule(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateDepreciationRule(ctx, newRule(1, "IT设备3年加速折旧", 36, "percent", 0.05, ladderStagesJSON))
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero id")
	}
	if !created.Enabled {
		t.Fatal("rule should default to enabled")
	}

	got, err := s.GetDepreciationRule(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("get rule: %v", err)
	}
	if got.Name != "IT设备3年加速折旧" || got.Months != 36 || got.FloorType != "percent" || got.FloorVal != 0.05 {
		t.Fatalf("unexpected rule: %+v", got)
	}
	if got.Stages != ladderStagesJSON {
		t.Fatalf("stages not preserved: %q", got.Stages)
	}
}

func TestCreateDepreciationRuleRejectsMissingFields(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	cases := []model.DepreciationRule{
		newRule(0, "x", 36, "percent", 0.05, ""),  // 公司边界必填
		newRule(1, "", 36, "percent", 0.05, ""),   // 名称必填
		newRule(1, "x", 0, "percent", 0.05, ""),   // 总月数必填
	}
	for i, r := range cases {
		if _, err := s.CreateDepreciationRule(ctx, r); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestGetDepreciationRuleCompanyBoundary(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateDepreciationRule(ctx, newRule(1, "规则A", 36, "percent", 0.05, ""))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.GetDepreciationRule(ctx, 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company get must be NotFound, got %v", err)
	}
	if _, err := s.GetDepreciationRule(ctx, 1, created.ID+999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing rule must be NotFound, got %v", err)
	}
}

func TestListDepreciationRules(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for _, name := range []string{"规则一", "规则二", "规则三"} {
		if _, err := s.CreateDepreciationRule(ctx, newRule(7, name, 24, "amount", 100, "")); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	// 别公司的规则不可见
	if _, err := s.CreateDepreciationRule(ctx, newRule(8, "别公司规则", 36, "percent", 0, "")); err != nil {
		t.Fatalf("create other company: %v", err)
	}

	items, total, err := s.ListDepreciationRules(ctx, DepreciationRuleListFilter{CompanyID: 7, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("expected 3 rules, got total=%d len=%d", total, len(items))
	}
	// 分页
	page1, total, err := s.ListDepreciationRules(ctx, DepreciationRuleListFilter{CompanyID: 7, Page: 1, PageSize: 2})
	if err != nil || total != 3 || len(page1) != 2 {
		t.Fatalf("pagination: total=%d len=%d err=%v", total, len(page1), err)
	}
	// 公司边界
	other, total, err := s.ListDepreciationRules(ctx, DepreciationRuleListFilter{CompanyID: 8, Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(other) != 1 || other[0].Name != "别公司规则" {
		t.Fatalf("company boundary violated: %v %d %v", other, total, err)
	}
}

func TestUpdateDepreciationRule(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateDepreciationRule(ctx, newRule(1, "旧名", 36, "percent", 0.05, ""))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated := created
	updated.Name = "新名"
	updated.Months = 48
	updated.FloorType = "amount"
	updated.FloorVal = 200
	updated.Stages = ladderStagesJSON
	updated.Enabled = false

	got, err := s.UpdateDepreciationRule(ctx, updated)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Name != "新名" || got.Months != 48 || got.FloorType != "amount" || got.FloorVal != 200 || got.Enabled {
		t.Fatalf("update not applied: %+v", got)
	}

	// 越权公司更新 → NotFound；缺主键 → 错误
	wrong := updated
	wrong.CompanyID = 2
	if _, err := s.UpdateDepreciationRule(ctx, wrong); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company update must be NotFound, got %v", err)
	}
	noID := updated
	noID.ID = 0
	if _, err := s.UpdateDepreciationRule(ctx, noID); err == nil {
		t.Fatal("update without id must fail")
	}
}

func TestDeleteDepreciationRule(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateDepreciationRule(ctx, newRule(1, "待删", 36, "percent", 0.05, ""))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 越权公司删除 → NotFound
	if err := s.DeleteDepreciationRule(ctx, 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company delete must be NotFound, got %v", err)
	}
	if err := s.DeleteDepreciationRule(ctx, 1, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetDepreciationRule(ctx, 1, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted rule must be NotFound, got %v", err)
	}
	// 幂等删除
	if err := s.DeleteDepreciationRule(ctx, 1, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete must be NotFound, got %v", err)
	}
}
