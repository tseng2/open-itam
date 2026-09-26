package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// GeoIP 二期 · 异地漫游告警（geo_roaming）：双通道（WebHook + 站内信）
// 双键隔离（geo_roaming / notify_geo_roaming）+ 独立冷却窗（不复用
// webhook 配置的 cooldown_minutes）+ Run 启动即扫——与 overdue/missing
// 同引擎同表（webhook_alert_states 唯一索引天然隔离），零新表零新引擎

func initGeoEngineDB(t *testing.T) (*store.GormStore, time.Time) {
	t.Helper()
	db, err := store.InitDB("sqlite", t.TempDir()+"/webhook_geo_test.db")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return store.NewGormStore(db), time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
}

// seedGeoRoamFixture 公司（基准 广东省|东莞市）+ 漫游资产（苏州出口
// 222.92.0.1，地理维漫游）+ 失联资产（对照：基础冷却窗）
func seedGeoRoamFixture(t *testing.T, base time.Time) (companyID int64) {
	t.Helper()
	company := model.Company{Name: "GeoRoam 告警测试公司", Code: "GEO"}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}

	// 漫游资产：东莞公司跑到苏州出口（last_seen 必须新鲜——五态优先级
	// overdue > missing > roaming，stale 夹具会判 missing 拿不到漫游告警）
	roam := model.Asset{CompanyID: company.ID, CategoryID: 2, AssetTag: "GEO-ROAM", Status: 20}
	store.DB.Create(&roam)
	store.DB.Create(&model.Device{AssetID: &roam.ID, DeviceID: "dev-geo-roam",
		LastSeenAt: base.Add(-5 * time.Minute), IPAddress: "192.168.1.10", PublicIP: "222.92.0.1"})

	// 失联资产：对照——走 webhook 配置的基础冷却窗
	missing := model.Asset{CompanyID: company.ID, CategoryID: 2, AssetTag: "GEO-MISSING", Status: 20}
	store.DB.Create(&missing)
	store.DB.Create(&model.Device{AssetID: &missing.ID, DeviceID: "dev-geo-missing",
		LastSeenAt: base.Add(-2 * time.Hour)})

	return company.ID
}

// newGeoEngine 引擎组装：geoFn 按夹具公司注入「省|市」基准（苏州出口
// 222.92.0.1 regionprobe 实测样本）；geoWindow ≤ 0 → 默认 24h 回落
func newGeoEngine(t *testing.T, st *store.GormStore, base time.Time, companyID int64, notifier AlertNotifier, geoWindow time.Duration) *Engine {
	t.Helper()
	engine := NewEngine(store.DB, st,
		func() time.Duration { return 15 * time.Minute },
		func([]int64) model.PresenceGeo {
			return model.PresenceGeo{
				RegionByCompany: map[int64]string{companyID: "广东省|东莞市"},
				RegionOf: func(ip string) (string, string, string) {
					if ip == "222.92.0.1" {
						return "中国", "江苏省", "苏州市"
					}
					return "", "", ""
				},
			}
		},
		func() time.Duration { return geoWindow },
		notifier)
	engine.now = func() time.Time { return base }
	return engine
}

// 双通道双键隔离：geo_roaming 经 WebHook 批量推送 + 站内信独立投递，
// 冷却状态落 geo_roaming / notify_geo_roaming 双键，与既有类型互不干扰
func TestScanOnceGeoRoamingDualChannelAndDualKeys(t *testing.T) {
	rec := &notifyRecorder{}
	st, base := initGeoEngineDB(t)
	companyID := seedGeoRoamFixture(t, base)
	engine := newGeoEngine(t, st, base, companyID, rec.handle, 0)
	push := newPushRecorder(http.StatusOK)
	enableWebhook(t, st, push.srv.URL, "", 60)

	n, err := engine.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("scan once: %v", err)
	}
	// 站内信 2（geo+missing）+ WebHook 2（geo+missing 一次批量）= 4
	if n != 4 || rec.count() != 2 || push.count() != 1 {
		t.Fatalf("dual channel: n=%d notify=%d push=%d", n, rec.count(), push.count())
	}

	// 站内信收到 geo_roaming 告警，文案带判定依据（出口解析区域 + 公司基准）
	var roamAlert *Alert
	rec.mu.Lock()
	for i := range rec.alerts {
		if rec.alerts[i].AlertType == model.WebhookAlertGeoRoaming {
			roamAlert = &rec.alerts[i]
		}
	}
	rec.mu.Unlock()
	if roamAlert == nil {
		t.Fatalf("notifier must receive geo_roaming alert, got %+v", rec.alerts)
	}
	if !strings.Contains(roamAlert.Message, "江苏省|苏州市") ||
		!strings.Contains(roamAlert.Message, "广东省|东莞市") ||
		!strings.Contains(roamAlert.Message, "222.92.0.1") {
		t.Fatalf("geo message must carry region basis: %q", roamAlert.Message)
	}

	// WebHook 批量体含 geo_roaming
	var payload struct {
		Alerts []Alert `json:"alerts"`
	}
	if err := json.Unmarshal(push.last().Body, &payload); err != nil {
		t.Fatalf("decode push: %v", err)
	}
	found := false
	for _, a := range payload.Alerts {
		if a.AlertType == model.WebhookAlertGeoRoaming {
			found = true
		}
	}
	if !found {
		t.Fatalf("webhook push must include geo_roaming: %+v", payload.Alerts)
	}

	// 冷却状态：两通道 × 两类型 = 4 键（geo_roaming 与 notify_geo_roaming
	// 双键 + missing 双键），唯一索引天然隔离
	types := stateAlertTypes(t, st)
	if types[model.WebhookAlertGeoRoaming] != 1 ||
		types[NotifyAlertType(model.WebhookAlertGeoRoaming)] != 1 ||
		types[model.WebhookAlertMissing] != 1 ||
		types[NotifyAlertType(model.WebhookAlertMissing)] != 1 ||
		len(types) != 4 {
		t.Fatalf("dual-key states mismatch: %+v", types)
	}
}

// 独立冷却窗：基础窗（cooldown_minutes=60）到期后 missing 重投而
// geo_roaming 仍在自身窗口内被抑制；漫游窗到期才再投
func TestScanOnceGeoRoamingIndependentCooldownWindow(t *testing.T) {
	rec := &notifyRecorder{}
	st, base := initGeoEngineDB(t)
	companyID := seedGeoRoamFixture(t, base)
	engine := newGeoEngine(t, st, base, companyID, rec.handle, 2*time.Hour)

	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("first scan must deliver 2, got n=%d err=%v", n, err)
	}
	if rec.count() != 2 {
		t.Fatalf("first scan deliveries: %d", rec.count())
	}

	// 前移 61 分钟（基础窗已过、漫游窗 2h 未到）：missing 再投、geo 抑制；
	// 漫游资产心跳同步刷新保持 roaming（stale 会翻成 missing）
	base = base.Add(61 * time.Minute)
	engine.now = func() time.Time { return base }
	if err := store.DB.Model(&model.Device{}).
		Where("device_id = ?", "dev-geo-roam").
		Update("last_seen_at", base).Error; err != nil {
		t.Fatalf("refresh roam heartbeat: %v", err)
	}
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("only missing must re-deliver after base window, got n=%d err=%v", n, err)
	}
	rec.mu.Lock()
	lastAlert := rec.alerts[len(rec.alerts)-1]
	rec.mu.Unlock()
	if lastAlert.AlertType != model.WebhookAlertMissing {
		t.Fatalf("re-delivered alert must be missing, got %q", lastAlert.AlertType)
	}

	// 再前移越过漫游窗（2h）：geo_roaming 重投
	base = base.Add(2*time.Hour + time.Minute)
	engine.now = func() time.Time { return base }
	if err := store.DB.Model(&model.Device{}).
		Where("device_id = ?", "dev-geo-roam").
		Update("last_seen_at", base).Error; err != nil {
		t.Fatalf("refresh roam heartbeat again: %v", err)
	}
	if n, err := engine.ScanOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("both must re-deliver after geo window, got n=%d err=%v", n, err)
	}
}

// Run 启动即扫：漫游告警在启动扫描轮立即补投，不空等周期
func TestRunStartupScanDeliversGeoRoaming(t *testing.T) {
	rec := &notifyRecorder{}
	st, base := initGeoEngineDB(t)
	companyID := seedGeoRoamFixture(t, base)
	engine := newGeoEngine(t, st, base, companyID, rec.handle, 0)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		engine.Run(ctx, 20*time.Millisecond)
		close(done)
	}()
	deadline := time.After(5 * time.Second)
	for rec.count() < 2 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("startup scan must deliver 2 geo/missing alerts, got %d", rec.count())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must exit promptly after cancel")
	}
}
