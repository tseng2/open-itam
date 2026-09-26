package softwareaudit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// 阶段三软件合规引擎单测：受控池 × device_software 安装聚合 → 超用投递 +
// 冷却去重（webhook_alert_states 同表 software_overuse 键，asset_id 列存
// 池项 ID）+ 未挂许可不限席位 + 投递失败不落状态下轮重试 + 未注入空转

func setupAuditEngine(t *testing.T, notifier Notifier) (*Engine, *store.GormStore) {
	t.Helper()
	db, err := store.InitDB("sqlite", t.TempDir()+"/softwareaudit_test.db")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	st := store.NewGormStore(db)
	return NewEngine(db, st, 0, notifier), st // cooldown=0 → 默认 24h
}

type overuseNotify struct {
	Pool       model.SoftwarePool
	Installs   int64
	TotalSeats int64
}

type overuseRecorder struct {
	mu    sync.Mutex
	calls []overuseNotify
	fail  bool
}

func (rec *overuseRecorder) handle(ctx context.Context, p model.SoftwarePool, installs, totalSeats int64) error {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.fail {
		return errors.New("notifier down")
	}
	rec.calls = append(rec.calls, overuseNotify{Pool: p, Installs: installs, TotalSeats: totalSeats})
	return nil
}

func (rec *overuseRecorder) count() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.calls)
}

// seedAuditFixture 公司 A：超用池项（席位 2 / 安装 4）、合规池项（不限席位）、
// 无安装池项；公司 B：独立超用池项——证明扇出按公司互不干扰
func seedAuditFixture(t *testing.T) (companyA, companyB int64, overA, okA, emptyA, overB int64) {
	t.Helper()
	ca := model.Company{Name: "合规引擎测试公司A", Code: "SWA"}
	cb := model.Company{Name: "合规引擎测试公司B", Code: "SWB"}
	for _, c := range []*model.Company{&ca, &cb} {
		if err := store.DB.Create(c).Error; err != nil {
			t.Fatalf("seed company: %v", err)
		}
	}
	lic := model.License{CompanyID: ca.ID, Name: "M365 商业版", TotalSeats: 2}
	if err := store.DB.Create(&lic).Error; err != nil {
		t.Fatalf("seed license: %v", err)
	}

	pools := []model.SoftwarePool{
		{CompanyID: ca.ID, Name: "Microsoft 365", LicenseID: &lic.ID},
		{CompanyID: ca.ID, Name: "WPS Office"},
		{CompanyID: ca.ID, Name: "AutoCAD"},
	}
	licB := model.License{CompanyID: cb.ID, Name: "PS 授权", TotalSeats: 1}
	if err := store.DB.Create(&licB).Error; err != nil {
		t.Fatalf("seed license B: %v", err)
	}
	pools = append(pools, model.SoftwarePool{CompanyID: cb.ID, Name: "Adobe Photoshop", LicenseID: &licB.ID})
	for i := range pools {
		if err := store.DB.Create(&pools[i]).Error; err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}

	// 终端软件清单：devA1-devA4 装 M365（超用），devA1 装 WPS（合规），
	// devB1-devB2 装 PS（超用）；AutoCAD 无安装
	softwareRows := []model.DeviceSoftware{}
	for _, dev := range []string{"devA1", "devA2", "devA3", "devA4"} {
		softwareRows = append(softwareRows, model.DeviceSoftware{
			CompanyID: ca.ID, DeviceID: dev, AssetID: 1,
			Name: "Microsoft 365 Apps for enterprise", SeenAt: time.Now().UTC(),
		})
	}
	softwareRows = append(softwareRows, model.DeviceSoftware{
		CompanyID: ca.ID, DeviceID: "devA1", AssetID: 1,
		Name: "WPS Office 2023", SeenAt: time.Now().UTC(),
	})
	for _, dev := range []string{"devB1", "devB2"} {
		softwareRows = append(softwareRows, model.DeviceSoftware{
			CompanyID: cb.ID, DeviceID: dev, AssetID: 2,
			Name: "Adobe Photoshop 2026", SeenAt: time.Now().UTC(),
		})
	}
	for i := range softwareRows {
		if err := store.DB.Create(&softwareRows[i]).Error; err != nil {
			t.Fatalf("seed device software: %v", err)
		}
	}
	return ca.ID, cb.ID, pools[0].ID, pools[1].ID, pools[2].ID, pools[3].ID
}

func TestEngineDeliversOveruseAndCooldownDedup(t *testing.T) {
	rec := &overuseRecorder{}
	e, st := setupAuditEngine(t, rec.handle)
	companyA, companyB, overA, okA, emptyA, overB := seedAuditFixture(t)
	ctx := context.Background()

	n, err := e.ScanOnce(ctx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	// 两公司各一件超用；不限席位与零安装池项不投
	if n != 2 || rec.count() != 2 {
		t.Fatalf("sent=%d calls=%d, want 2", n, rec.count())
	}
	gotA, gotB := false, false
	for _, c := range rec.calls {
		if c.Pool.ID == overA {
			gotA = c.Installs == 4 && c.TotalSeats == 2 && c.Pool.CompanyID == companyA
		}
		if c.Pool.ID == overB {
			gotB = c.Installs == 2 && c.TotalSeats == 1 && c.Pool.CompanyID == companyB
		}
	}
	if !gotA || !gotB {
		t.Fatalf("overuse payloads wrong: %+v", rec.calls)
	}
	_ = okA
	_ = emptyA

	// 冷却去重：第二轮同超用不重复投
	if n, err := e.ScanOnce(ctx); err != nil || n != 0 || rec.count() != 2 {
		t.Fatalf("cooldown dedup failed: n=%d err=%v calls=%d", n, err, rec.count())
	}

	// 冷却过期后重新投递：把状态 SentAt 拨回 25h 前
	states, _ := st.ListWebhookAlertStates(ctx)
	for _, s := range states {
		if s.AlertType != StateAlertType {
			continue
		}
		if err := st.PutWebhookAlertState(ctx, model.WebhookAlertState{
			CompanyID: s.CompanyID, AssetID: s.AssetID, AlertType: s.AlertType,
			SentAt: time.Now().UTC().Add(-25 * time.Hour),
		}); err != nil {
			t.Fatalf("backdate state: %v", err)
		}
	}
	if n, err := e.ScanOnce(ctx); err != nil || n != 2 || rec.count() != 4 {
		t.Fatalf("cooldown expiry should re-deliver: n=%d err=%v calls=%d", n, err, rec.count())
	}
}

func TestEngineFailureRetriesAndNilNotifierNoop(t *testing.T) {
	ctx := context.Background()

	// 未注入：安静空转
	e, _ := setupAuditEngine(t, nil)
	seedAuditFixture(t)
	if n, err := e.ScanOnce(ctx); err != nil || n != 0 {
		t.Fatalf("nil notifier must noop: n=%d err=%v", n, err)
	}

	// 投递失败：不落冷却状态，下轮重试成功
	rec := &overuseRecorder{fail: true}
	e2, st := setupAuditEngine(t, rec.handle)
	seedAuditFixture(t)
	if n, err := e2.ScanOnce(ctx); err != nil || n != 0 {
		t.Fatalf("failed deliveries must not count: n=%d err=%v", n, err)
	}
	states, _ := st.ListWebhookAlertStates(ctx)
	for _, s := range states {
		if s.AlertType == StateAlertType {
			t.Fatal("failed delivery must not record cooldown state")
		}
	}
	rec.fail = false
	if n, err := e2.ScanOnce(ctx); err != nil || n != 2 {
		t.Fatalf("retry round must deliver: n=%d err=%v", n, err)
	}
}
