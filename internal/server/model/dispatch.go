package model

import "time"

// 外派登记状态机：超期不是独立状态，
// 而是 Status=DispatchStatusActive 且已过 ExpectedReturnAt 的计算属性（A2 失联分层依据）
const (
	DispatchStatusActive   = 10 // 外派中
	DispatchStatusReturned = 20 // 已归还
	DispatchStatusCanceled = 30 // 已作废
)

// 外派/归还动作在资产履历（AssetEvent）中留痕的事件类型
const (
	AssetEventDispatch       = "dispatch"        // 外派登记
	AssetEventDispatchReturn = "dispatch_return" // 外派归还
)

// AssetDispatch 外派登记：长期出差/涉密客户现场终端的"免死金牌"。
// IsolationOffline 是保密现场"预期内离线"的免责依据，ExpectWipe 服务于涉密归还后的
// 格式化检疫流程；一个资产同时只允许一条外派中记录（store 层唯一性校验）
type AssetDispatch struct {
	BaseModel
	CompanyID        int64      `gorm:"index;not null" json:"company_id"`
	AssetID          int64      `gorm:"index;not null" json:"asset_id"`
	Asset            *Asset     `gorm:"foreignKey:AssetID" json:"asset,omitempty"`
	BorrowerName     string     `gorm:"type:varchar(64);index" json:"borrower_name"` // 外派负责人
	Destination      string     `gorm:"type:varchar(128)" json:"destination"`         // 目的地/客户现场
	DispatchedAt     time.Time  `json:"dispatched_at"`                                // 外派日期
	ExpectedReturnAt time.Time  `gorm:"index" json:"expected_return_at"`              // 预计归期
	ReturnedAt       *time.Time `json:"returned_at,omitempty"`                        // 实际归还时间
	IsolationOffline bool       `json:"isolation_offline"`                            // 保密现场不可联网（预期内离线的依据）
	ExpectWipe       bool       `json:"expect_wipe"`                                  // 预期格式化归还（涉密客户要求）
	Status           int        `gorm:"default:10;index;not null" json:"status"`       // 10 外派中 / 20 已归还 / 30 已作废
	Remark           string     `gorm:"type:text" json:"remark"`
}

func (AssetDispatch) TableName() string { return "asset_dispatches" }
