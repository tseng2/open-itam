package model

import "time"

// P2 体验运营 · 消息中心起步（站内信）：面向"人"的待办与状态通知，
// 与操作日志（面向"审计"）正交。首个事件源为设备申请审批流：
// 提交 → 通知公司管理员（待办提醒）；通过/驳回 → 通知申请人（状态回执）。
// A4 Webhook 告警、盘点任务等事件源后续按场景接入；WebHook 单通道已在
// A4 落地（webhook_alert_config），站内信是消息中心的第二块地基。

// 通知类型常量：随事件源扩展；Resource/ResourceID 指回业务对象供前端跳转
const (
	NotificationTypeAssetRequest = "asset_request" // 设备申请：新申请待办 / 审批结果回执
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
