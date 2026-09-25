package licensealert

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// 阶段五收官 · 事件源扩展：软件许可到期提醒引擎单测。
// 窗口口径复用 store.ListLicenses 的 ExpiringDays 过滤（单源勿重写），
// 故不注入时钟——夹具以真实 now 为基准构造到期日。
// 去重复用 webhook_alert_states 同表（license_expiring 键，asset_id 列
// 存许可 ID）：同一许可窗口期内只提醒一次（冷却 = 窗口天数，许可在
// 窗口内至多停留 N 天，天然只投一次）

func setupEngine(t *testing.T, notifier Notifier) (*Engine, *store.GormStore) {
	t.Helper()
	db, err := store.InitDB("sqlite", t.TempDir()+"/licensealert_test.db")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	st := store.NewGormStore(db)
	return NewEngine(db, st, 0, notifier), st // windowDays=0 → 默认 30 天
}

type capturedNotify struct {
	License  model.License
	DaysLeft int
}

// notifyRecorder 到期提醒记录器：fail 置真模拟投递端故障
type notifyRecorder struct {
	mu      sync.Mutex
	calls   []capturedNotify
	fail    bool
}

func (rec *notifyRecorder) handle(ctx context.Context, l model.License, daysLeft int) error {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.fail {
		return errors.New("notifier down")
	}
	rec.calls = append(rec.calls, capturedNotify{License: l, DaysLeft: daysLeft})
	return nil
}

func (rec *notifyRecorder) count() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.calls)
}

func daysFromNow(d int) *time.Time {
	t := time.Now().UTC().AddDate(0, 0, d)
	return &t
}

// seedLicenseFixture 双公司五许可：只有 A 公司 20 天到期与 B 公司 5 天
// 到期的两件落在 30 天窗口内（窗口外/已终止/已过期/永久授权全部不投）
func seedLicenseFixture(t *testing.T) (companyA, companyB, expiringA, expiringB int64) {
	t.Helper()
	ca := model.Company{Name: "许可提醒测试公司A", Code: "LICA"}
	cb := model.Company{Name: "许可提醒测试公司B", Code: "LICB"}
	for _, c := range []*model.Company{&ca, &cb} {
		if err := store.DB.Create(c).Error; err != nil {
			t.Fatalf("seed company: %v", err)
		}
	}
	seed := []model.License{
		{CompanyID: ca.ID, Name: "Microsoft 365（窗口内）", ExpirationDate: daysFromNow(20)},
		{CompanyID: ca.ID, Name: "AutoCAD（窗口外 45 天）", ExpirationDate: daysFromNow(45)},
		{CompanyID: ca.ID, Name: "已终止许可", ExpirationDate: daysFromNow(10), TerminationDate: daysFromNow(-1)},
		{CompanyID: ca.ID, Name: "已过期许可", ExpirationDate: daysFromNow(-1)},
		{CompanyID: ca.ID, Name: "永久授权", ExpirationDate: nil},
		{CompanyID: cb.ID, Name: "JetBrains 全家桶（窗口内）", ExpirationDate: daysFromNow(5)},
	}
	for i := range seed {
		if err := store.DB.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed license: %v", err)
		}
	}
	return ca.ID, cb.ID, seed[0].ID, seed[5].ID
}

func licenseAlertStates(t *testing.T, st *store.GormStore) []model.WebhookAlertState {
	t.Helper()
	states, err := st.ListWebhookAlertStates(context.Background())
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	return states
}

func TestScanOnceNotifiesExpiringLicensesPerCompany(t *testing.T) {
	rec := &notifyRecorder{}
	engine, st := setupEngine(t, rec.handle)
	companyA, companyB, expiringA, expiringB := seedLicenseFixture(t)

	n, err := engine.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("scan once: %v", err)
	}
	if n != 2 || rec.count() != 2 {
		t.Fatalf("expected 2 notifications, got n=%d delivered=%d", n, rec.count())
	}

	// 两天数与公司边界正确；窗口外/已终止/已过期/永久授权全不投
	rec.mu.Lock()
	got := make(map[int64]capturedNotify, len(rec.calls))
	for _, c := range rec.calls {
		got[c.License.ID] = c
	}
	rec.mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("unexpected notify targets: %+v", got)
	}
	if _, ok := got[expiringA]; !ok {
		t.Fatalf("license %d (company A) must be notified: %+v", expiringA, got)
	}
	if _, ok := got[expiringB]; !ok {
		t.Fatalf("license %d (company B) must be notified: %+v", expiringB, got)
	}
	if got[expiringA].DaysLeft != 20 || got[expiringB].DaysLeft != 5 {
		t.Fatalf("days left mismatch: %+v", got)
	}
	if got[expiringA].License.CompanyID != companyA || got[expiringB].License.CompanyID != companyB {
		t.Fatalf("company mismatch: %+v", got)
	}

	// 冷却状态落库：license_expiring 键，asset_id 列存许可 ID
	states := licenseAlertStates(t, st)
	if len(states) != 2 {
		t.Fatalf("expected 2 cooldown states, got %d: %+v", len(states), states)
	}
	for _, s := range states {
		if s.AlertType != StateAlertType {
			t.Fatalf("state alert type: %+v", s)
		}
		if s.AssetID != expiringA && s.AssetID != expiringB {
			t.Fatalf("state license id: %+v", s)
		}
	}
}

func TestScanOnceWindowDedupSuppresses(t *testing.T) {
	rec := &notifyRecorder{}
	engine, st := setupEngine(t, rec.handle)
	seedLicenseFixture(t)

	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("first scan must notify 2, got n=%d err=%v", n, err)
	}
	// 窗口期内再扫（冷却 = 窗口天数）：同一许可只提醒一次
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("second scan must be suppressed, got n=%d err=%v", n, err)
	}
	if rec.count() != 2 {
		t.Fatalf("no extra delivery expected, got %d", rec.count())
	}
	if len(licenseAlertStates(t, st)) != 2 {
		t.Fatalf("states must stay stable at 2")
	}
}

func TestScanOnceNotifierFailureRetriesNextRound(t *testing.T) {
	rec := &notifyRecorder{fail: true}
	engine, st := setupEngine(t, rec.handle)
	seedLicenseFixture(t)

	// 投递端故障：不落冷却状态（下一轮重试）
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("failed notify must deliver 0 without error, got n=%d err=%v", n, err)
	}
	if len(licenseAlertStates(t, st)) != 0 {
		t.Fatalf("failed notify must not record state, got %+v", licenseAlertStates(t, st))
	}

	// 投递端恢复：同一轮数据可补投
	rec.mu.Lock()
	rec.fail = false
	rec.mu.Unlock()
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("recovered notify must deliver 2, got n=%d err=%v", n, err)
	}
	if len(licenseAlertStates(t, st)) != 2 {
		t.Fatalf("recovered notify must record states")
	}
}

func TestScanOnceQuietWithoutLicenses(t *testing.T) {
	rec := &notifyRecorder{}
	engine, st := setupEngine(t, rec.handle)

	n, err := engine.ScanOnce(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("no licenses must be silent no-op, got n=%d err=%v", n, err)
	}
	if rec.count() != 0 || len(licenseAlertStates(t, st)) != 0 {
		t.Fatalf("unexpected deliveries")
	}
}

func TestScanOnceNilNotifierIsNoop(t *testing.T) {
	engine, _ := setupEngine(t, nil)
	seedLicenseFixture(t)
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("nil notifier must be no-op, got n=%d err=%v", n, err)
	}
}

func TestRunScansPeriodicallyAndStopsOnCancel(t *testing.T) {
	rec := &notifyRecorder{}
	engine, _ := setupEngine(t, rec.handle)
	seedLicenseFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		engine.Run(ctx, 20*time.Millisecond) // 测试注入高频扫描间隔
		close(done)
	}()

	deadline := time.After(5 * time.Second)
	tick := time.After(0)
	for rec.count() < 2 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("Run did not notify within timeout, delivered=%d", rec.count())
		case <-tick:
			tick = time.After(20 * time.Millisecond)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must exit promptly after ctx cancel")
	}
	// 后续轮次被窗口去重抑制，不重复轰炸
	if got := rec.count(); got != 2 {
		t.Fatalf("dedup must hold across rounds, got %d", got)
	}
}
