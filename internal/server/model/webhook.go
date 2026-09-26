package model

import "time"

// Webhook 告警类型（阶段五 A4）：与联系状态高危态一一对应；
// test 仅用于 Web UI 手动连通性测试，不出现在定时扫描产物中；
// geo_roaming 为异地漫游告警（2026-09-26 GeoIP 二期）：漫游频次
// 语义与失联催报不同，冷却窗独立计（geoCooldownFn 注入）
const (
	WebhookAlertOverdue   = "overdue"   // 超期未归（外派中且已过预计归期）
	WebhookAlertMissing   = "missing"   // 疑似失联（无外派豁免且心跳超阈值）
	WebhookAlertGeoRoaming = "geo_roaming" // 异地漫游（网络维本机公网 / 地理维异地 / 海外出口）
	WebhookAlertTest      = "test"      // 手动连通性测试
)

// DefaultWebhookCooldownMinutes 告警冷却窗口默认 60 分钟：
// 同资产同类型未处理期间不重复轰炸，冷却期满仍未恢复则再次提醒；
// 窗口时长是配置（存 DB，Web UI 可改），禁止硬编码散落到业务逻辑
const DefaultWebhookCooldownMinutes = 60

// WebhookAlertConfig 超期/失联 Webhook 告警的全局配置（单例，固定 ID=1）。
// 与 protection_modules 同模式存 DB、Web UI 配置，严禁塞 server.json；
// secret 非空时出站请求带 HMAC-SHA256 签名头，永不经 API 回传（json:"-"）
type WebhookAlertConfig struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"` // 单例固定 1
	Enabled         bool      `gorm:"not null;default:false" json:"enabled"`
	WebhookURL      string    `gorm:"type:varchar(512);not null;default:''" json:"webhook_url"`
	Secret          string    `gorm:"type:varchar(255);not null;default:''" json:"-"`
	CooldownMinutes int       `gorm:"not null;default:60" json:"cooldown_minutes"` // 冷却窗口（分钟）
	UpdatedAt       time.Time `gorm:"not null" json:"updated_at"`
}

func (WebhookAlertConfig) TableName() string { return "webhook_alert_config" }

// WebhookAlertState 告警冷却状态：记录同资产同类型最近一次推送时间，
// 冷却窗口内不重复推送（去重/防轰炸的唯一依据，进程重启不丢）
type WebhookAlertState struct {
	BaseModel
	CompanyID int64     `gorm:"uniqueIndex:idx_webhook_alert_state;not null" json:"company_id"`
	AssetID   int64     `gorm:"uniqueIndex:idx_webhook_alert_state;not null" json:"asset_id"`
	AlertType string    `gorm:"type:varchar(32);uniqueIndex:idx_webhook_alert_state;not null" json:"alert_type"` // overdue / missing / geo_roaming
	SentAt    time.Time `gorm:"not null" json:"sent_at"`
}

func (WebhookAlertState) TableName() string { return "webhook_alert_states" }
