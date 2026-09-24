package model

import "time"

// 设备申请状态机（CIYO 对标语义）：pending → approved / rejected / canceled
const (
	AssetRequestStatusPending  = 10 // 待审批
	AssetRequestStatusApproved = 20 // 已通过（审批即绑定领用人，同事务）
	AssetRequestStatusRejected = 30 // 已驳回
	AssetRequestStatusCanceled = 40 // 已取消（申请人撤回）
)

// AssetEventAssign 领用/派发履历事件（asset_events 既有语义，借此常量化收口）
const AssetEventAssign = "assign"

// AssetRequest 设备申请：申请人在提交时即指定目标资产（从库存中选），
// 审批通过在同一事务内完成「申请状态流转 + 资产绑定领用人 + 台账状态 10→20
// + 领用履历」——CIYO「审批+调拨同事务」精髓，无中间态悬挂；
// 短期借用必须带归期（ReturnDate 同时落履历），长期领用归期留空
type AssetRequest struct {
	BaseModel
	CompanyID     int64  `gorm:"index;not null" json:"company_id"`
	AssetID       int64  `gorm:"index;not null" json:"asset_id"`
	Asset         *Asset `gorm:"foreignKey:AssetID" json:"asset,omitempty"` // GormStore 列表预载；SQLiteStore 测试库无 assets 表不预载
	ApplicantID   int64  `gorm:"index;not null" json:"applicant_id"`       // 申请人员（User.ID）
	ApplicantName string `gorm:"type:varchar(128);index" json:"applicant_name"` // 姓名快照：用户改名/离职不影响历史展示
	IsLongTerm    bool   `json:"is_long_term"`                              // 长期领用；false = 短期借用（必须带归期）
	ExpectedReturnAt *time.Time `json:"expected_return_at,omitempty"`       // 短期借用预计归还时间
	Reason        string `gorm:"type:varchar(255)" json:"reason"`           // 申请事由
	Status        int    `gorm:"default:10;index;not null" json:"status"`
	// ApprovedBy/ApprovedAt 承载"决策人"语义：审批通过与驳回都记录
	ApprovedBy     *int64     `json:"approved_by"`
	ApprovedAt     *time.Time `json:"approved_at,omitempty"`
	DecisionRemark string     `gorm:"type:varchar(255)" json:"decision_remark"` // 审批批注 / 驳回原因
}

func (AssetRequest) TableName() string { return "asset_requests" }
