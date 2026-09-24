package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

type capturedPush struct {
	Body       []byte
	Signature  string
	AuthHeader string
}

type pushRecorder struct {
	mu   sync.Mutex
	push []capturedPush
	srv  *httptest.Server
}

func newPushRecorder(status int) *pushRecorder {
	rec := &pushRecorder{}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.push = append(rec.push, capturedPush{
			Body:       body,
			Signature:  r.Header.Get(SignatureHeader),
			AuthHeader: r.Header.Get("Authorization"),
		})
		rec.mu.Unlock()
		w.WriteHeader(status)
	}))
	return rec
}

func (rec *pushRecorder) count() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.push)
}

func (rec *pushRecorder) last() capturedPush {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.push[len(rec.push)-1]
}

func setupEngine(t *testing.T) (*Engine, *store.GormStore, time.Time) {
	t.Helper()
	db, err := store.InitDB("sqlite", t.TempDir()+"/webhook_engine_test.db")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	st := store.NewGormStore(db)
	base := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	engine := NewEngine(db, st, 15*time.Minute)
	engine.now = func() time.Time { return base }
	return engine, st, base
}

func seedAlertFixture(t *testing.T, base time.Time) (missingAssetID, overdueAssetID, onlineAssetID int64) {
	t.Helper()
	company := model.Company{Name: "Webhook 告警测试公司", Code: "WHT"}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}

	// 失联资产：无外派豁免 + 心跳超阈值
	missing := model.Asset{CompanyID: company.ID, CategoryID: 2, AssetTag: "WH-MISSING", Status: 20}
	store.DB.Create(&missing)
	store.DB.Create(&model.Device{AssetID: &missing.ID, DeviceID: "dev-missing", LastSeenAt: base.Add(-2 * time.Hour)})

	// 超期资产：外派中且已过预计归期
	overdue := model.Asset{CompanyID: company.ID, CategoryID: 2, AssetTag: "WH-OVERDUE", Status: 20}
	store.DB.Create(&overdue)
	store.DB.Create(&model.Device{AssetID: &overdue.ID, DeviceID: "dev-overdue", LastSeenAt: base.Add(-5 * time.Minute)})
	store.DB.Create(&model.AssetDispatch{
		CompanyID: company.ID, AssetID: overdue.ID, BorrowerName: "李四",
		Destination: "上海客户现场", DispatchedAt: base.Add(-72 * time.Hour),
		ExpectedReturnAt: base.Add(-1 * time.Hour), Status: model.DispatchStatusActive,
	})

	// 健康资产：在线无外派 → 不应产生告警
	online := model.Asset{CompanyID: company.ID, CategoryID: 2, AssetTag: "WH-ONLINE", Status: 20}
	store.DB.Create(&online)
	store.DB.Create(&model.Device{AssetID: &online.ID, DeviceID: "dev-online", LastSeenAt: base.Add(-1 * time.Minute)})

	return missing.ID, overdue.ID, online.ID
}

func enableWebhook(t *testing.T, st *store.GormStore, url, secret string, cooldownMinutes int) {
	t.Helper()
	if err := st.PutWebhookAlertConfig(context.Background(), model.WebhookAlertConfig{
		Enabled: true, WebhookURL: url, Secret: secret, CooldownMinutes: cooldownMinutes,
	}); err != nil {
		t.Fatalf("enable webhook: %v", err)
	}
}

func TestScanOncePushesOverdueAndMissingWithSignature(t *testing.T) {
	engine, st, base := setupEngine(t)
	missingID, overdueID, _ := seedAlertFixture(t, base)
	rec := newPushRecorder(http.StatusOK)
	enableWebhook(t, st, rec.srv.URL, "topsecret", model.DefaultWebhookCooldownMinutes)

	n, err := engine.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("scan once: %v", err)
	}
	if n != 2 || rec.count() != 1 {
		t.Fatalf("expected 2 alerts in 1 push, got n=%d pushes=%d", n, rec.count())
	}

	push := rec.last()
	if push.Signature == "" {
		t.Fatal("push must carry HMAC signature when secret configured")
	}
	if push.AuthHeader != "" {
		t.Fatal("push must not carry credentials in Authorization header")
	}
	if want := SignBody("topsecret", push.Body); push.Signature != want {
		t.Fatalf("signature mismatch: got %s want %s", push.Signature, want)
	}

	var payload struct {
		Event  string          `json:"event"`
		Alerts []map[string]any `json:"alerts"`
	}
	if err := json.Unmarshal(push.Body, &payload); err != nil {
		t.Fatalf("decode push: %v", err)
	}
	if payload.Event != EventTypeAlert || len(payload.Alerts) != 2 {
		t.Fatalf("unexpected payload: %s", push.Body)
	}
	types := map[string]float64{}
	for _, a := range payload.Alerts {
		types[a["alert_type"].(string)] = a["asset_id"].(float64)
	}
	if types[model.WebhookAlertMissing] != float64(missingID) || types[model.WebhookAlertOverdue] != float64(overdueID) {
		t.Fatalf("alert targets mismatch: %+v", types)
	}

	// 推送成功必须落冷却状态（进程重启后也不重复轰炸）
	states, err := st.ListWebhookAlertStates(context.Background())
	if err != nil || len(states) != 2 {
		t.Fatalf("expected 2 cooldown states, got %d err=%v", len(states), err)
	}
}

func TestScanOnceCooldownSuppressesAndRepushes(t *testing.T) {
	engine, st, base := setupEngine(t)
	seedAlertFixture(t, base)
	rec := newPushRecorder(http.StatusOK)
	enableWebhook(t, st, rec.srv.URL, "", 60)

	if _, err := engine.ScanOnce(context.Background()); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("first scan must push once, got %d", rec.count())
	}

	// 冷却窗口内再扫：不重复推送
	if n, _ := engine.ScanOnce(context.Background()); n != 0 {
		t.Fatalf("inside cooldown must suppress, got %d", n)
	}
	if rec.count() != 1 {
		t.Fatalf("no push expected inside cooldown, got %d", rec.count())
	}

	// 时间前移越过冷却窗口：仍未处理 → 再次提醒。
	// 健康资产同步刷新心跳（模拟持续在线），隔离时钟前移的干扰
	base = base.Add(61 * time.Minute)
	engine.now = func() time.Time { return base }
	if err := store.DB.Model(&model.Device{}).
		Where("device_id = ?", "dev-online").
		Update("last_seen_at", base).Error; err != nil {
		t.Fatalf("refresh online heartbeat: %v", err)
	}

	n, err := engine.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("scan after cooldown: %v", err)
	}
	if n != 2 || rec.count() != 2 {
		t.Fatalf("expected re-push of 2 alerts, got n=%d pushes=%d", n, rec.count())
	}
}

func TestScanOnceSkipsWhenDisabledOrUnconfigured(t *testing.T) {
	engine, st, base := setupEngine(t)
	seedAlertFixture(t, base)
	rec := newPushRecorder(http.StatusOK)

	// 未配置：安静空转
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("unconfigured must be silent no-op, got n=%d err=%v", n, err)
	}

	// 配了地址但未启用：同样不推
	enableWebhook(t, st, rec.srv.URL, "", 60)
	if err := st.PutWebhookAlertConfig(context.Background(), model.WebhookAlertConfig{
		Enabled: false, WebhookURL: rec.srv.URL, CooldownMinutes: 60,
	}); err != nil {
		t.Fatalf("disable webhook: %v", err)
	}
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("disabled must be silent no-op, got n=%d err=%v", n, err)
	}
	if rec.count() != 0 {
		t.Fatalf("no push expected, got %d", rec.count())
	}
}

func TestScanOnceFailedPushRetriesNextRound(t *testing.T) {
	engine, st, base := setupEngine(t)
	seedAlertFixture(t, base)
	rec := newPushRecorder(http.StatusInternalServerError) // 接收端持续 5xx
	enableWebhook(t, st, rec.srv.URL, "", 60)

	if _, err := engine.ScanOnce(context.Background()); err == nil {
		t.Fatal("receiver error must surface from ScanOnce")
	}

	// 发送失败不得记录冷却状态：下一轮重试
	states, err := st.ListWebhookAlertStates(context.Background())
	if err != nil || len(states) != 0 {
		t.Fatalf("failed push must not record cooldown state, got %d err=%v", len(states), err)
	}

	// 接收端恢复后同一轮数据可重推
	rec2 := newPushRecorder(http.StatusOK)
	enableWebhook(t, st, rec2.srv.URL, "", 60)
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("expected retry push of 2, got n=%d err=%v", n, err)
	}
	_ = base
}

func TestRunScansPeriodicallyAndStopsOnCancel(t *testing.T) {
	engine, st, base := setupEngine(t)
	seedAlertFixture(t, base)
	rec := newPushRecorder(http.StatusOK)
	enableWebhook(t, st, rec.srv.URL, "", model.DefaultWebhookCooldownMinutes)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		engine.Run(ctx, 20*time.Millisecond) // 测试注入高频扫描间隔
		close(done)
	}()

	// 首轮 tick 内必须完成一次推送；后续轮次被冷却窗口抑制（每轮仍会扫）
	deadline := time.After(5 * time.Second)
	tick := time.After(0)
	for rec.count() < 1 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("Run did not push within timeout, pushes=%d", rec.count())
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
	// 退出后不再产生新推送
	if got := rec.count(); got < 1 {
		t.Fatalf("expected at least one push, got %d", got)
	}
}
