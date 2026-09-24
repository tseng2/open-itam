package depreciation

import (
	"context"
	"testing"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

const engineLadder = `[{"period":12,"unit":"MONTH","ratio":0.35},{"period":12,"unit":"MONTH","ratio":0.30},{"period":12,"unit":"MONTH","ratio":0.30}]`

func setupEngine(t *testing.T) (*Engine, time.Time) {
	t.Helper()
	db, err := store.InitDB("sqlite", t.TempDir()+"/depreciation_engine_test.db")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	// 固定时钟：2026-09-24，跨月边界测试可控
	return NewEngine(db), time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
}

func seedCompany(t *testing.T) int64 {
	t.Helper()
	c := model.Company{Name: "折旧引擎测试公司-" + t.Name() + "-" + time.Now().Format("150405.000000000")}
	if err := store.DB.Create(&c).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	return c.ID
}

func seedRule(t *testing.T, companyID int64, stages string, enabled bool) int64 {
	t.Helper()
	r := model.DepreciationRule{
		CompanyID: companyID,
		Name:      "3年加速折旧",
		Months:    36,
		FloorType: "percent",
		FloorVal:  0.05,
		Stages:    stages,
		Enabled:   enabled,
	}
	if err := store.DB.Create(&r).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	return r.ID
}

func seedAsset(t *testing.T, companyID, ruleID int64, tag string, purchaseDate *time.Time, price, netValue float64) int64 {
	t.Helper()
	a := model.Asset{
		CompanyID:      companyID,
		CategoryID:     2,
		CategoryName:   "笔记本",
		AssetTag:       tag,
		Status:         model.AssetStatusInUse,
		PurchaseDate:   purchaseDate,
		OriginalPrice:  price,
		NetValue:       netValue,
		DepreciationID: nil,
	}
	if ruleID > 0 {
		a.DepreciationID = &ruleID
	}
	if err := store.DB.Create(&a).Error; err != nil {
		t.Fatalf("seed asset %s: %v", tag, err)
	}
	return a.ID
}

func assetNetValue(t *testing.T, id int64) float64 {
	t.Helper()
	var a model.Asset
	if err := store.DB.Select("id", "net_value").First(&a, id).Error; err != nil {
		t.Fatalf("load asset %d: %v", id, err)
	}
	return a.NetValue
}

func TestScanOnceRefreshesNetValue(t *testing.T) {
	e, now := setupEngine(t)
	ctx := context.Background()
	companyID := seedCompany(t)
	ruleID := seedRule(t, companyID, engineLadder, true)

	// 购置 30 个月（2024-03-24 → 2026-09-24）：阶梯累计 0.8 → 净值 2000
	purchase := now.AddDate(0, -30, 0)
	id := seedAsset(t, companyID, ruleID, "AST-DEP-001", &purchase, 10000, 0)

	n, err := e.ScanOnce(ctx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 update, got %d", n)
	}
	if got := assetNetValue(t, id); got != 2000 {
		t.Fatalf("net value = %v, want 2000", got)
	}

	// 第二轮幂等：净值已正确不再写库
	n, err = e.ScanOnce(ctx)
	if err != nil || n != 0 {
		t.Fatalf("second scan should be no-op, got %d err=%v", n, err)
	}
}

func TestScanOnceResidualFloor(t *testing.T) {
	e, now := setupEngine(t)
	ctx := context.Background()
	companyID := seedCompany(t)
	ruleID := seedRule(t, companyID, engineLadder, true)

	// 购置 48 个月：阶梯累计 0.95，残值率 5% 兜底 → 净值 500
	purchase := now.AddDate(0, -48, 0)
	id := seedAsset(t, companyID, ruleID, "AST-DEP-002", &purchase, 10000, 9999)

	n, err := e.ScanOnce(ctx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 update, got %d", n)
	}
	if got := assetNetValue(t, id); got != 500 {
		t.Fatalf("residual floor: net value = %v, want 500", got)
	}
}

func TestScanOnceSkips(t *testing.T) {
	e, now := setupEngine(t)
	ctx := context.Background()
	companyID := seedCompany(t)
	ruleID := seedRule(t, companyID, engineLadder, true)

	purchase := now.AddDate(0, -30, 0)
	// 无规则挂接 / 缺购置日期 / 原值非正：三类资产全部不动
	noRule := seedAsset(t, companyID, 0, "AST-DEP-NORULE", &purchase, 10000, 123)
	noDate := seedAsset(t, companyID, ruleID, "AST-DEP-NODATE", nil, 10000, 456)
	noPrice := seedAsset(t, companyID, ruleID, "AST-DEP-NOPRICE", &purchase, 0, 789)

	n, err := e.ScanOnce(ctx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 updates, got %d", n)
	}
	for id, want := range map[int64]float64{noRule: 123, noDate: 456, noPrice: 789} {
		if got := assetNetValue(t, id); got != want {
			t.Fatalf("asset %d net value changed: %v, want %v", id, got, want)
		}
	}
}

func TestScanOnceDisabledRuleFrozen(t *testing.T) {
	e, now := setupEngine(t)
	ctx := context.Background()
	companyID := seedCompany(t)
	ruleID := seedRule(t, companyID, engineLadder, false) // 停用

	purchase := now.AddDate(0, -30, 0)
	id := seedAsset(t, companyID, ruleID, "AST-DEP-DISABLED", &purchase, 10000, 777)

	n, err := e.ScanOnce(ctx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 0 {
		t.Fatalf("disabled rule must not refresh, got %d updates", n)
	}
	if got := assetNetValue(t, id); got != 777 {
		t.Fatalf("net value changed under disabled rule: %v", got)
	}
}

func TestScanOnceCrossCompanyReferenceIgnored(t *testing.T) {
	e, now := setupEngine(t)
	ctx := context.Background()
	companyA := seedCompany(t)
	companyB := seedCompany(t)
	ruleID := seedRule(t, companyA, engineLadder, true)

	// B 公司资产悬空引用 A 公司规则：公司边界防御，不刷新
	purchase := now.AddDate(0, -30, 0)
	id := seedAsset(t, companyB, ruleID, "AST-DEP-CROSS", &purchase, 10000, 321)

	n, err := e.ScanOnce(ctx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 0 {
		t.Fatalf("cross-company reference must be ignored, got %d updates", n)
	}
	if got := assetNetValue(t, id); got != 321 {
		t.Fatalf("net value changed across company boundary: %v", got)
	}
}

func TestScanOnceStraightLineRule(t *testing.T) {
	e, now := setupEngine(t)
	ctx := context.Background()
	companyID := seedCompany(t)
	// stages 为空：36 个月直线折旧，残值率 5%
	ruleID := seedRule(t, companyID, "", true)

	purchase := now.AddDate(0, -18, 0) // 18 个月折半
	id := seedAsset(t, companyID, ruleID, "AST-DEP-LINE", &purchase, 12000, 0)

	n, err := e.ScanOnce(ctx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 update, got %d", n)
	}
	if got := assetNetValue(t, id); got != 6000 {
		t.Fatalf("straight line: net value = %v, want 6000", got)
	}
}

func TestRunExitsOnCancel(t *testing.T) {
	e, _ := setupEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx, 10*time.Millisecond)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("engine Run did not exit after cancel")
	}
}
