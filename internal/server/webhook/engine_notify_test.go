package webhook

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// 阶段五收官 · 事件源扩展：A4 告警联动站内信通道的单测。
// 站内信独立于 WebHook 开关（系统内通道，WebHook 未配置也该收到）；
// 冷却去重复用 webhook_alert_states 同表，notify_* 独立键与 WebHook
// 通道互不干扰；投递失败不落冷却状态，下一轮重试（旁路语义）

// notifyRecorder 站内信投递记录器：fail 置真模拟投递端故障
type notifyRecorder struct {
	mu     sync.Mutex
	alerts []Alert
	fail   bool
}

func (rec *notifyRecorder) handle(ctx context.Context, a Alert) error {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.fail {
		return errors.New("notifier down")
	}
	rec.alerts = append(rec.alerts, a)
	return nil
}

func (rec *notifyRecorder) count() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.alerts)
}

func setupNotifyEngine(t *testing.T, notifier AlertNotifier) (*Engine, *store.GormStore, time.Time) {
	t.Helper()
	db, err := store.InitDB("sqlite", t.TempDir()+"/webhook_notify_test.db")
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
	engine := NewEngine(db, st, 15*time.Minute, notifier)
	engine.now = func() time.Time { return base }
	return engine, st, base
}

// stateAlertTypes 归集冷却状态表里的告警类型集合
func stateAlertTypes(t *testing.T, st *store.GormStore) map[string]int {
	t.Helper()
	states, err := st.ListWebhookAlertStates(context.Background())
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	types := make(map[string]int, len(states))
	for _, s := range states {
		types[s.AlertType]++
	}
	return types
}

func TestScanOnceNotifiesWithoutWebhookConfig(t *testing.T) {
	rec := &notifyRecorder{}
	engine, st, base := setupNotifyEngine(t, rec.handle)
	missingID, overdueID, _ := seedAlertFixture(t, base)

	// WebHook 完全未配置（默认关闭）→ 站内信通道仍然投递
	n, err := engine.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("scan once: %v", err)
	}
	if n != 2 || rec.count() != 2 {
		t.Fatalf("expected 2 notify deliveries, got n=%d delivered=%d", n, rec.count())
	}

	// 回调收到的是原始告警（判定口径与 WebHook 通道同源）
	seen := map[int64]string{}
	rec.mu.Lock()
	for _, a := range rec.alerts {
		seen[a.AssetID] = a.AlertType
	}
	rec.mu.Unlock()
	if seen[missingID] != model.WebhookAlertMissing || seen[overdueID] != model.WebhookAlertOverdue {
		t.Fatalf("notified alerts mismatch: %+v", seen)
	}

	// 独立冷却键落库（notify_ 前缀，与 WebHook 通道键隔离）
	types := stateAlertTypes(t, st)
	if types[NotifyAlertType(model.WebhookAlertMissing)] != 1 ||
		types[NotifyAlertType(model.WebhookAlertOverdue)] != 1 ||
		len(types) != 2 {
		t.Fatalf("notify cooldown states mismatch: %+v", types)
	}
}

func TestScanOnceNotifyCooldownSuppressesAndRepushes(t *testing.T) {
	rec := &notifyRecorder{}
	engine, st, base := setupNotifyEngine(t, rec.handle)
	seedAlertFixture(t, base)

	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("first scan must notify 2, got n=%d err=%v", n, err)
	}
	// WebHook 未配置：状态表只有站内信通道的独立键
	if types := stateAlertTypes(t, st); len(types) != 2 {
		t.Fatalf("notify-only states: %+v", types)
	}
	// 冷却窗口内再扫：不重复投递
	if n, _ := engine.ScanOnce(context.Background()); n != 0 {
		t.Fatalf("inside cooldown must suppress, got %d", n)
	}
	if rec.count() != 2 {
		t.Fatalf("no extra delivery expected inside cooldown, got %d", rec.count())
	}

	// 时间前移越过冷却窗口：仍未处理 → 再次提醒（健康资产心跳同步刷新，
	// 隔离时钟前移把它变成新失联告警的干扰）
	base = base.Add(61 * time.Minute)
	engine.now = func() time.Time { return base }
	if err := store.DB.Model(&model.Device{}).
		Where("device_id = ?", "dev-online").
		Update("last_seen_at", base).Error; err != nil {
		t.Fatalf("refresh online heartbeat: %v", err)
	}
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("expected re-notify of 2, got n=%d err=%v", n, err)
	}
	if rec.count() != 4 {
		t.Fatalf("re-notify deliveries: %d", rec.count())
	}
}

func TestScanOnceNotifyAndWebhookChannelsIndependent(t *testing.T) {
	rec := &notifyRecorder{}
	engine, st, base := setupNotifyEngine(t, rec.handle)
	seedAlertFixture(t, base)
	push := newPushRecorder(http.StatusOK)
	enableWebhook(t, st, push.srv.URL, "", 60)

	// 双通道同时投递：站内信 2 条 + WebHook 一次批量（2 告警）
	n, err := engine.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 4 || rec.count() != 2 || push.count() != 1 {
		t.Fatalf("both channels: n=%d notify=%d push=%d", n, rec.count(), push.count())
	}
	// 同表四条状态：两通道各自计窗互不干扰
	types := stateAlertTypes(t, st)
	if len(types) != 4 {
		t.Fatalf("expected 4 independent states, got %+v", types)
	}

	// 冷却窗口内再扫：两通道都静默
	if n, _ := engine.ScanOnce(context.Background()); n != 0 {
		t.Fatalf("both channels must suppress inside cooldown, got %d", n)
	}

	// 关闭 WebHook 开关 + 越过冷却窗：站内信照常再投，WebHook 不推
	if err := st.PutWebhookAlertConfig(context.Background(), model.WebhookAlertConfig{
		Enabled: false, WebhookURL: push.srv.URL, CooldownMinutes: 60,
	}); err != nil {
		t.Fatalf("disable webhook: %v", err)
	}
	base = base.Add(61 * time.Minute)
	engine.now = func() time.Time { return base }
	if err := store.DB.Model(&model.Device{}).
		Where("device_id = ?", "dev-online").
		Update("last_seen_at", base).Error; err != nil {
		t.Fatalf("refresh online heartbeat: %v", err)
	}
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("notify must survive webhook switch-off, got n=%d err=%v", n, err)
	}
	if rec.count() != 4 || push.count() != 1 {
		t.Fatalf("webhook must stay off after switch-off: notify=%d push=%d", rec.count(), push.count())
	}
}

func TestScanOnceNotifyFailureRetriesAndKeepsWebhookAlive(t *testing.T) {
	rec := &notifyRecorder{fail: true}
	engine, st, base := setupNotifyEngine(t, rec.handle)
	seedAlertFixture(t, base)
	push := newPushRecorder(http.StatusOK)
	enableWebhook(t, st, push.srv.URL, "", 60)

	// 站内信投递端故障：不落冷却状态（下轮重试），也绝不拖垮 WebHook
	n, err := engine.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("notify failure is bypass, must not fail scan: %v", err)
	}
	if n != 2 || push.count() != 1 {
		t.Fatalf("webhook channel must deliver independently: n=%d push=%d", n, push.count())
	}
	types := stateAlertTypes(t, st)
	if len(types) != 2 { // 只有 WebHook 通道的 overdue/missing
		t.Fatalf("failed notify must not record state, got %+v", types)
	}

	// 投递端恢复：站内信补投（WebHook 通道被自身冷却抑制）
	rec.mu.Lock()
	rec.fail = false
	rec.mu.Unlock()
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("recovered notify must deliver, got n=%d err=%v", n, err)
	}
	if rec.count() != 2 || push.count() != 1 {
		t.Fatalf("recovered deliveries: notify=%d push=%d", rec.count(), push.count())
	}
	_ = base
}
