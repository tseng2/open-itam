package webhook

import (
	"fmt"
	"time"

	"itagent/internal/server/model"
)

// 推送事件类型：定时扫描告警 / Web UI 手动连通性测试
const (
	EventTypeAlert = "itam.alert"
	EventTypeTest = "itam.test"
)

// Alert 单条告警载荷：超期未归带外派上下文（催归要用），
// 疑似失联带最近心跳（核实要用），异地漫游带判定依据与最近心跳
type Alert struct {
	AssetID          int64      `json:"asset_id"`
	CompanyID        int64      `json:"company_id"`
	AssetTag         string     `json:"asset_tag"`
	Model            string     `json:"model,omitempty"`
	AlertType        string     `json:"alert_type"` // overdue / missing / geo_roaming
	BorrowerName     string     `json:"borrower_name,omitempty"`
	Destination      string     `json:"destination,omitempty"`
	ExpectedReturnAt *time.Time `json:"expected_return_at,omitempty"`
	LastSeenAt       *time.Time `json:"last_seen_at,omitempty"`
	Message          string     `json:"message"`
}

// AlertKey 冷却去重的唯一键：同资产同类型才互相抑制
type AlertKey struct {
	CompanyID int64
	AssetID   int64
	AlertType string
}

// Payload 一轮扫描的完整推送体（批量合并为一次 POST，减少接收端压力）
type Payload struct {
	Event     string    `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	Alerts    []Alert   `json:"alerts"`
}

// BuildAlerts 从台账资产构建超期/失联告警清单。联系状态判定复用
// model.ResolveAssetPresence（内部即 model.ResolvePresence 纯函数），
// 阈值与漫游地理基准由调用方经函数注入（唯一源 agent_settings），
// 禁止在本包重复实现
func BuildAlerts(assets []model.Asset, dispatchByAsset map[int64]*model.AssetDispatch, now time.Time, heartbeatTimeout time.Duration, geo model.PresenceGeo) []Alert {
	model.ResolveAssetPresence(assets, dispatchByAsset, now, heartbeatTimeout, geo)

	alerts := make([]Alert, 0, len(assets))
	for i := range assets {
		a := &assets[i]
		switch a.Presence {
		case model.PresenceOverdue:
			// presence=overdue 必然持有进行中外派记录，安全解引用
			d := dispatchByAsset[a.ID]
			expected := d.ExpectedReturnAt
			alerts = append(alerts, Alert{
				AssetID:          a.ID,
				CompanyID:        a.CompanyID,
				AssetTag:         a.AssetTag,
				Model:            a.ModelName,
				AlertType:        model.WebhookAlertOverdue,
				BorrowerName:     d.BorrowerName,
				Destination:      d.Destination,
				ExpectedReturnAt: &expected,
				Message: fmt.Sprintf("外派超期未归：资产 %s（%s）负责人 %s，目的地 %s，预计归期 %s",
					a.AssetTag, a.ModelName, d.BorrowerName, d.Destination,
					expected.Format("2006-01-02 15:04")),
			})
		case model.PresenceMissing:
			lastSeen := a.Device.LastSeenAt
			alerts = append(alerts, Alert{
				AssetID:    a.ID,
				CompanyID:  a.CompanyID,
				AssetTag:   a.AssetTag,
				Model:      a.ModelName,
				AlertType:  model.WebhookAlertMissing,
				LastSeenAt: &lastSeen,
				Message: fmt.Sprintf("疑似失联：资产 %s（%s）最近心跳 %s，已超离线阈值无心跳",
					a.AssetTag, a.ModelName, lastSeen.Format("2006-01-02 15:04:05")),
			})
		case model.PresenceRoaming:
			// 异地漫游（GeoIP 二期）：判定依据直读 Asset.RoamingReason
			//（ResolveAssetPresence roaming 分支写回，口径单源禁止重算）。
			// 文案四要素：资产编码 + 所属公司区域 + 判定依据 + 最近心跳
			lastSeen := a.Device.LastSeenAt
			home := geo.RegionByCompany[a.CompanyID]
			if home == "" {
				home = "未配置"
			}
			alerts = append(alerts, Alert{
				AssetID:    a.ID,
				CompanyID:  a.CompanyID,
				AssetTag:   a.AssetTag,
				Model:      a.ModelName,
				AlertType:  model.WebhookAlertGeoRoaming,
				LastSeenAt: &lastSeen,
				Message: fmt.Sprintf("异地漫游：资产 %s（%s）所属公司区域 %s，%s；最近心跳 %s",
					a.AssetTag, a.ModelName, home, a.RoamingReason,
					lastSeen.Format("2006-01-02 15:04:05")),
			})
		}
	}
	return alerts
}

// FilterDue 冷却去重：同资产同类型在冷却窗口内不重复推送；
// 冷却期满仍未恢复（未处理）则放行再次提醒；类型升级（missing→overdue）
// 独立计窗。冷却时长按告警类型取（geo_roaming 独立计窗，其余走
// webhook 配置的基础窗）——cooldownFor 由调用方组装
func FilterDue(alerts []Alert, lastSent map[AlertKey]time.Time, cooldownFor func(alertType string) time.Duration, now time.Time) []Alert {
	due := make([]Alert, 0, len(alerts))
	for _, a := range alerts {
		if sent, ok := lastSent[AlertKey{a.CompanyID, a.AssetID, a.AlertType}]; ok && now.Sub(sent) < cooldownFor(a.AlertType) {
			continue
		}
		due = append(due, a)
	}
	return due
}

// TestPayload Web UI"手动测试"按钮的连通性载荷（event=itam.test）
func TestPayload(now time.Time) Payload {
	return Payload{
		Event:     EventTypeTest,
		Timestamp: now,
		Alerts: []Alert{{
			AlertType: model.WebhookAlertTest,
			Message:   "Open-ITAM Webhook 连通性测试：收到本条即通道与签名校验正常",
		}},
	}
}
