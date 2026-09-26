package webhook

import (
	"encoding/json"
	"strings"
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
	fixedCooldown := func(string) time.Duration { return cooldown }

	alerts := []Alert{
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertOverdue},
		{CompanyID: 1, AssetID: 2, AlertType: model.WebhookAlertMissing},
	}
	lastSent := map[AlertKey]time.Time{
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertOverdue}: now.Add(-30 * time.Minute), // 窗口内：抑制
		{CompanyID: 1, AssetID: 2, AlertType: model.WebhookAlertMissing}: now.Add(-2 * time.Hour),   // 窗口外：再提醒
	}

	due := FilterDue(alerts, lastSent, fixedCooldown, now)
	if len(due) != 1 || due[0].AssetID != 2 {
		t.Fatalf("expected only asset 2 due after cooldown, got %+v", due)
	}

	// 全部在冷却内 → 空推送
	lastSent[AlertKey{CompanyID: 1, AssetID: 2, AlertType: model.WebhookAlertMissing}] = now.Add(-10 * time.Minute)
	if due := FilterDue(alerts, lastSent, fixedCooldown, now); len(due) != 0 {
		t.Fatalf("expected empty push inside cooldown, got %+v", due)
	}

	// 冷却边界：恰好在窗口边缘（差 1ns 仍抑制，到期即放行）
	lastSent[AlertKey{CompanyID: 1, AssetID: 2, AlertType: model.WebhookAlertMissing}] = now.Add(-cooldown)
	if due := FilterDue(alerts, lastSent, fixedCooldown, now); len(due) != 1 {
		t.Fatalf("cooldown exactly elapsed must re-push, got %+v", due)
	}
}

// 按类型冷却（geo_roaming 独立计窗）：同资产 geo_roaming 在自身窗口内抑制，
// 即便其他类型的窗口已过；窗口由 cooldownFor 按告警类型取
func TestFilterDuePerTypeCooldownWindow(t *testing.T) {
	now, _ := alertFixture()
	alerts := []Alert{
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertOverdue},   // 基础窗 1h
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertGeoRoaming}, // 漫游窗 24h
	}
	lastSent := map[AlertKey]time.Time{
		// 两类型 2 小时前都推过：基础窗已过（放行），漫游窗仍在（抑制）
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertOverdue}:    now.Add(-2 * time.Hour),
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertGeoRoaming}: now.Add(-2 * time.Hour),
	}
	cooldownFor := func(alertType string) time.Duration {
		if alertType == model.WebhookAlertGeoRoaming {
			return 24 * time.Hour
		}
		return time.Hour
	}

	due := FilterDue(alerts, lastSent, cooldownFor, now)
	if len(due) != 1 || due[0].AlertType != model.WebhookAlertOverdue {
		t.Fatalf("geo_roaming must stay suppressed inside its own window, got %+v", due)
	}

	// 漫游窗到期即放行
	lastSent[AlertKey{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertGeoRoaming}] = now.Add(-24 * time.Hour)
	if due := FilterDue(alerts, lastSent, cooldownFor, now); len(due) != 2 {
		t.Fatalf("geo_roaming must be due after its own window elapses, got %+v", due)
	}
}

func TestFilterDueDifferentTypeOrCompanyIsIndependent(t *testing.T) {
	now, _ := alertFixture()
	cooldown := time.Hour
	fixedCooldown := func(string) time.Duration { return cooldown }
	alerts := []Alert{
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertMissing},
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertOverdue}, // 同资产不同类型：状态升级，独立推送
	}
	lastSent := map[AlertKey]time.Time{
		{CompanyID: 1, AssetID: 1, AlertType: model.WebhookAlertMissing}: now,
	}
	due := FilterDue(alerts, lastSent, fixedCooldown, now)
	if len(due) != 1 || due[0].AlertType != model.WebhookAlertOverdue {
		t.Fatalf("type escalation must be independent of cooldown, got %+v", due)
	}
}

// geo_roaming 告警（GeoIP 二期）：presence=roaming 产出第六类告警，
// 文案四要素 = 资产编码 + 所属公司区域 + 判定依据（RoamingReason 直读
// 禁止重算）+ 最近心跳
func TestBuildAlertsGeoRoaming(t *testing.T) {
	now, threshold := alertFixture()

	// 地理维漫游：东莞公司资产跑到苏州出口
	geoRoaming := model.Asset{
		BaseModel: model.BaseModel{ID: 11}, CompanyID: 7, AssetTag: "ITAM-ROAM-1", ModelName: "ThinkPad X1",
		Device: &model.Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.10", PublicIP: "222.92.0.1"},
	}
	// 网络维漫游：本机直接持有公网 IP（公司区域未配置也不影响网络维）
	netRoaming := model.Asset{
		BaseModel: model.BaseModel{ID: 12}, CompanyID: 7, AssetTag: "ITAM-ROAM-2", ModelName: "MacBook Pro",
		Device: &model.Device{
			LastSeenAt: now.Add(-3 * time.Minute), IPAddress: "223.104.5.6", PublicIP: "223.104.5.6"},
	}
	geo := model.PresenceGeo{
		RegionByCompany: map[int64]string{7: "广东省|东莞市"},
		RegionOf: func(ip string) (string, string, string) {
			if ip == "222.92.0.1" {
				return "中国", "江苏省", "苏州市"
			}
			return "", "", ""
		},
	}

	alerts := BuildAlerts([]model.Asset{geoRoaming, netRoaming}, nil, now, threshold, geo)
	if len(alerts) != 2 {
		t.Fatalf("expected 2 roaming alerts, got %d: %+v", len(alerts), alerts)
	}
	for _, a := range alerts {
		if a.AlertType != model.WebhookAlertGeoRoaming {
			t.Fatalf("roaming asset must emit geo_roaming alert, got %+v", a)
		}
		if a.CompanyID != 7 || a.LastSeenAt == nil {
			t.Fatalf("geo alert must carry company and last heartbeat: %+v", a)
		}
		if !strings.Contains(a.Message, a.AssetTag) || !strings.Contains(a.Message, "广东省|东莞市") {
			t.Fatalf("message must carry asset tag and home region: %q", a.Message)
		}
	}
	// 判定依据直读 RoamingReason（口径单源）：地理维带出口解析区域，网络维带本机公网 IP
	if !strings.Contains(alerts[0].Message, "江苏省|苏州市") || !strings.Contains(alerts[0].Message, "222.92.0.1") {
		t.Fatalf("geo-dim message must carry egress basis: %q", alerts[0].Message)
	}
	if !strings.Contains(alerts[1].Message, "223.104.5.6") || !strings.Contains(alerts[1].Message, "公网") {
		t.Fatalf("network-dim message must carry local ip basis: %q", alerts[1].Message)
	}
	// 最近心跳：文案携带心跳时刻
	if !strings.Contains(alerts[0].Message, now.Add(-5*time.Minute).Format("2006-01-02 15:04:05")) {
		t.Fatalf("message must carry last heartbeat time: %q", alerts[0].Message)
	}
}

// 在线资产（公司出口 + 本机私网）不得产出任何告警——漫游告警不误报
func TestBuildAlertsOnlineStaysSilent(t *testing.T) {
	now, threshold := alertFixture()
	online := model.Asset{
		BaseModel: model.BaseModel{ID: 21}, CompanyID: 7, AssetTag: "ITAM-ONLINE", ModelName: "ThinkPad T14",
		Device: &model.Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.10", PublicIP: "61.142.9.88"},
	}
	geo := model.PresenceGeo{
		RegionByCompany: map[int64]string{7: "广东省|东莞市"},
		RegionOf: func(ip string) (string, string, string) {
			if ip == "61.142.9.88" {
				return "中国", "广东省", "东莞市"
			}
			return "", "", ""
		},
	}
	alerts := BuildAlerts([]model.Asset{online}, nil, now, threshold, geo)
	if len(alerts) != 0 {
		t.Fatalf("online asset must not alert, got %+v", alerts)
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
