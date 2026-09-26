package webhook

import (
	"encoding/json"
	"testing"
	"time"

	"itagent/internal/server/model"
)

func alertFixture() (time.Time, time.Duration) {
	return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), 15 * time.Minute
}

func assetWithDevice(id int64, lastSeen time.Time) model.Asset {
	return model.Asset{
		BaseModel: model.BaseModel{ID: id},
		CompanyID: 1,
		AssetTag:  "ITAM-" + string(rune('0'+id)),
		ModelName: "ThinkPad X1",
		Device:    &model.Device{LastSeenAt: lastSeen},
	}
}

func TestBuildAlertsOverdueAndMissingOnly(t *testing.T) {
	now, threshold := alertFixture()

	overdue := assetWithDevice(1, now.Add(-5*time.Minute)) // 在线但外派超期 → overdue（催归优先）
	missing := assetWithDevice(2, now.Add(-time.Hour))     // 心跳超阈值且无豁免 → missing
	online := assetWithDevice(3, now.Add(-1*time.Minute))   // 健康 → 不告警
	isolated := assetWithDevice(4, now.Add(-time.Hour))    // 外派隔离未超期 → 预期内，免告警
	noDevice := model.Asset{BaseModel: model.BaseModel{ID: 5}} // 无 Agent 终端 → 不参与

	dispatches := map[int64]*model.AssetDispatch{
		1: {Status: model.DispatchStatusActive, ExpectedReturnAt: now.Add(-time.Hour),
			BorrowerName: "张三", Destination: "深圳客户现场"},
		4: {Status: model.DispatchStatusActive, ExpectedReturnAt: now.Add(24 * time.Hour), IsolationOffline: true},
	}

	alerts := BuildAlerts([]model.Asset{overdue, missing, online, isolated, noDevice}, dispatches, now, threshold, model.PresenceGeo{})
	if len(alerts) != 2 {
		t.Fatalf("expected 2 alerts (overdue+missing), got %d: %+v", len(alerts), alerts)
	}

	var gotOverdue, gotMissing *Alert
	for i := range alerts {
		switch alerts[i].AlertType {
		case model.WebhookAlertOverdue:
			gotOverdue = &alerts[i]
		case model.WebhookAlertMissing:
			gotMissing = &alerts[i]
		}
	}
	if gotOverdue == nil || gotMissing == nil {
		t.Fatalf("missing alert types: %+v", alerts)
	}

	if gotOverdue.AssetID != 1 || gotOverdue.BorrowerName != "张三" ||
		gotOverdue.Destination != "深圳客户现场" || gotOverdue.ExpectedReturnAt == nil {
		t.Fatalf("overdue alert incomplete: %+v", *gotOverdue)
	}
	if gotMissing.AssetID != 2 || gotMissing.LastSeenAt == nil {
		t.Fatalf("missing alert incomplete: %+v", *gotMissing)
	}
	if gotOverdue.Message == "" || gotMissing.Message == "" {
		t.Fatal("alert message must be human readable")
	}
}

func TestFilterDueCooldownWindow(t *testing.T) {
	now, _ := alertFixture()
	cooldown := time.Hour

	alerts := []Alert{
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertOverdue},
		{CompanyID: 1, AssetID: 2, AlertType: model.WebhookAlertMissing},
	}
	lastSent := map[AlertKey]time.Time{
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertOverdue}: now.Add(-30 * time.Minute), // 窗口内：抑制
		{CompanyID: 1, AssetID: 2, AlertType: model.WebhookAlertMissing}: now.Add(-2 * time.Hour),   // 窗口外：再提醒
	}

	due := FilterDue(alerts, lastSent, cooldown, now)
	if len(due) != 1 || due[0].AssetID != 2 {
		t.Fatalf("expected only asset 2 due after cooldown, got %+v", due)
	}

	// 全部在冷却内 → 空推送
	lastSent[AlertKey{CompanyID: 1, AssetID: 2, AlertType: model.WebhookAlertMissing}] = now.Add(-10 * time.Minute)
	if due := FilterDue(alerts, lastSent, cooldown, now); len(due) != 0 {
		t.Fatalf("expected empty push inside cooldown, got %+v", due)
	}

	// 冷却边界：恰好在窗口边缘（差 1ns 仍抑制，到期即放行）
	lastSent[AlertKey{CompanyID: 1, AssetID: 2, AlertType: model.WebhookAlertMissing}] = now.Add(-cooldown)
	if due := FilterDue(alerts, lastSent, cooldown, now); len(due) != 1 {
		t.Fatalf("cooldown exactly elapsed must re-push, got %+v", due)
	}
}

func TestFilterDueDifferentTypeOrCompanyIsIndependent(t *testing.T) {
	now, _ := alertFixture()
	cooldown := time.Hour
	alerts := []Alert{
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertMissing},
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertOverdue}, // 同资产不同类型：状态升级，独立推送
	}
	lastSent := map[AlertKey]time.Time{
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertMissing}: now,
	}
	due := FilterDue(alerts, lastSent, cooldown, now)
	if len(due) != 1 || due[0].AlertType != model.WebhookAlertOverdue {
		t.Fatalf("type escalation must be independent of cooldown, got %+v", due)
	}
}

func TestPayloadJSONShape(t *testing.T) {
	ts := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	p := Payload{Event: EventTypeAlert, Timestamp: ts, Alerts: []Alert{{
		AssetID: 7, CompanyID: 1, AssetTag: "ITAM-007", AlertType: model.WebhookAlertOverdue,
		Message: "外派超期未归",
	}}}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var back struct {
		Event  string  `json:"event"`
		Alerts []Alert `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if back.Event != "itam.alert" || len(back.Alerts) != 1 || back.Alerts[0].AssetID != 7 {
		t.Fatalf("unexpected payload shape: %s", raw)
	}
}
