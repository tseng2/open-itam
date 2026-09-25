package model

import "time"

// P2 体验运营 · 操作日志：管理端全部写操作与登录事件的追加式审计流水。
// 回答"谁在什么时候对什么对象做了什么、结果如何、从哪里来"的追溯问题，
// 与 AssetEvent（资产维度履历）正交：本表记录"管理动作"，不绑定单一资产。
// 追加式不可变（无更新/删除面），故不挂 BaseModel 的软删除语义。

// 操作动作常量：HTTP 方法缺省动作三件套；其余动作词由路径尾段派生
// （如 /dispatches/5/return → return、/assets/import → import），
// 登录事件在公开认证面单独留痕
const (
	OperationActionCreate      = "create"
	OperationActionUpdate      = "update"
	OperationActionDelete      = "delete"
	OperationActionLogin       = "login"
	OperationActionLoginFailed = "login_failed"
)

// OperationLog 单条审计记录。
// CompanyID=0 表示全局面操作（系统配置、登录等无公司上下文的动作）；
// Username/Role 为操作人快照——用户改名/离职不影响历史展示（沿申请人姓名快照先例）
type OperationLog struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CompanyID  int64     `gorm:"index;not null;default:0" json:"company_id"`
	UserID     int64     `gorm:"index;not null;default:0" json:"user_id"`
	Username   string    `gorm:"type:varchar(128);not null;default:''" json:"username"`
	Role       string    `gorm:"type:varchar(32);not null;default:''" json:"role"`
	Action     string    `gorm:"type:varchar(32);index;not null" json:"action"`
	Resource   string    `gorm:"type:varchar(64);index;not null" json:"resource"`
	ResourceID string    `gorm:"type:varchar(64);index;not null;default:''" json:"resource_id"`
	Path       string    `gorm:"type:varchar(255);not null;default:''" json:"path"` // 原始路径兜底（含多级子资源歧义）
	Detail     string    `gorm:"type:text" json:"detail"`                           // 请求体摘要（JSON 截断），multipart 不采集
	IP         string    `gorm:"type:varchar(64);not null;default:''" json:"ip"`
	UserAgent  string    `gorm:"type:varchar(255);not null;default:''" json:"user_agent"`
	Status     int       `gorm:"not null;default:0" json:"status"` // HTTP 状态码：2xx 成功，其余为失败尝试
	CreatedAt  time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (OperationLog) TableName() string { return "operation_logs" }
