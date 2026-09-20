package model

// AssetEvent 记录资产全生命周期事件（如调拨、维修、硬件升级、报废变更等）
type AssetEvent struct {
	BaseModel
	AssetID      int64   `gorm:"index;not null" json:"asset_id"`                    // 关联资产ID
	Asset        Asset   `gorm:"foreignKey:AssetID" json:"asset,omitempty"`
	EventType    string  `gorm:"type:varchar(32);index;not null" json:"event_type"` // assign(派发), return(归还), repair(维修), hardware_change(硬件变动), scrap(报废)
	Title        string  `gorm:"type:varchar(128);not null" json:"title"`           // 事件简述
	Description  string  `gorm:"type:text" json:"description"`                      // 详情或变化前后的JSON对比
	Cost         float64 `gorm:"type:decimal(10,2)" json:"cost"`                    // 维修或配件增减产生的金额
	OANumber     string  `gorm:"type:varchar(64)" json:"oa_number"`                 // 关联的OA申请单号
	ReviewStatus int     `gorm:"default:10;index;not null" json:"review_status"`    // 审核状态 (10:已完成/无需审核, 20:待审核, 30:已驳回)
	OperatorID   *int64  `gorm:"index" json:"operator_id"`                          // 操作人员或IT管理员ID
	Operator     *User   `gorm:"foreignKey:OperatorID" json:"operator,omitempty"`
}

func (AssetEvent) TableName() string {
	return "asset_events"
}
