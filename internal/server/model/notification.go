package model

import "time"

// P2 体验运营 · 消息中心起步（站内信）：面向"人"的待办与状态通知，
// 与操作日志（面向"审计"）正交。事件源：设备申请审批流（提交待办/
// 结果回执）、A4 告警联动（超期未归/疑似失联）、耗材低库存沿触发、
// 软件许可到期窗口扫描——投递目标为公司管理员或申请人本人。
// WebHook 单通道在 A4 落地（webhook_alert_config），站内信是第二通道。

// 通知类型常量：随事件源扩展；Resource/ResourceID 指回业务对象供前端跳转
const (
	NotificationTypeAssetRequest       = "asset_request"        // 设备申请：新申请待办 / 审批结果回执
	NotificationTypeConsumableLowStock = "consumable_low_stock" // 耗材库存预警：触线沿触发提醒
	NotificationTypeAssetAlert         = "asset_alert"          // 资产告警：超期未归 / 疑似失联（A4 引擎联动）
	NotificationTypeLicenseExpiring   = "license_expiring"     // 软件许可到期提醒：ExpiringDays 窗口扫描
)

// Notification 单条站内信。UserID 是收件人（只读自己的收件箱），
// ReadAt 为 NULL 表示未读；标记已读后可重复标记（幂等，鼓励语义）
type Notification struct {
	BaseModel
	CompanyID  int64      `gorm:"index;not null" json:"company_id"`
	UserID     int64      `gorm:"index;not null" json:"user_id"`
	Type       string     `gorm:"type:varchar(32);index;not null" json:"type"`
	Title      string     `gorm:"type:varchar(128);not null" json:"title"`
	Content    string     `gorm:"type:text" json:"content"`
	Resource   string     `gorm:"type:varchar(64);not null;default:''" json:"resource"`   // 关联对象面（如 asset-requests）
	ResourceID string     `gorm:"type:varchar(64);not null;default:''" json:"resource_id"` // 关联对象 ID
	ReadAt     *time.Time `gorm:"index" json:"read_at"`                                  // NULL = 未读
}

func (Notification) TableName() string { return "notifications" }
