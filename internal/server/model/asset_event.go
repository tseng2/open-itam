package model

import "time"

// 事件类型常量（盘点核对等新代码引用；历史字符串就地沿用同一值）
const AssetEventHardwareChange = "hardware_change"

// 财务销账（列管资产）事件类型：折旧完且财务销账的资产转"列管"继续跟踪，
// 恢复在册则反向流转；off_book = 销账转列管，off_book_restore = 恢复在册
const (
	AssetEventOffBook        = "off_book"
	AssetEventOffBookRestore = "off_book_restore"
)

// 履历审核状态段位（此前仅散落在注释与裸数字中，P0-β 盘点核对需消费待审事件，常量化收口）
const (
	AssetEventReviewDone    = 10 // 已完成/无需审核
	AssetEventReviewPending = 20 // 待审核
	AssetEventReviewRejected = 30 // 已驳回
)

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
	// 履历与配件流转详情
	TargetPerson   string     `gorm:"type:varchar(64)" json:"target_person"`              // 领用/责任人员
	PartType       string     `gorm:"type:varchar(32)" json:"part_type"`                  // 配件类别（内存/硬盘/显卡/外设等）
	PartModel      string     `gorm:"type:varchar(128)" json:"part_model"`                // 配件规格型号
	Quantity       int        `gorm:"default:1" json:"quantity"`                          // 数量
	LockerLocation string     `gorm:"type:varchar(64)" json:"locker_location"`            // IT储物柜位置
	WarrantyExpiry *time.Time `json:"warranty_expiry"`                                    // 维修质保截止日期
	ReturnDate     *time.Time `json:"return_date"`                                        // 待归还日期
	NetValue       float64    `gorm:"type:decimal(10,2)" json:"net_value"`                // 发生时净值
	ReviewStatus   int        `gorm:"default:10;index;not null" json:"review_status"`      // 审核状态 (10:已完成/无需审核, 20:待审核, 30:已驳回)
	OperatorID     *int64     `gorm:"index" json:"operator_id"`                            // 操作人员或IT管理员ID
	Operator       *User      `gorm:"foreignKey:OperatorID" json:"operator,omitempty"`
}

func (AssetEvent) TableName() string {
	return "asset_events"
}
