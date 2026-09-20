package model

import "time"

// AssetRepair 对应IT设备外寄维修登记表
type AssetRepair struct {
	BaseModel
	AssetID      int64      `gorm:"index;not null" json:"asset_id"`             // 关联固定资产ID
	Asset        Asset      `gorm:"foreignKey:AssetID" json:"asset,omitempty"`
	OANumber     string     `gorm:"type:varchar(64);index" json:"oa_number"`     // OA申请单号
	UserName     string     `gorm:"type:varchar(64)" json:"user_name"`           // 送修时使用人 (如: IT闲置、张三)
	FaultReason  string     `gorm:"type:text" json:"fault_reason"`               // 故障原因
	Diagnosis    string     `gorm:"type:text" json:"diagnosis"`                  // IT诊断结果
	Suggestion   string     `gorm:"type:text" json:"suggestion"`                 // IT维修建议
	Vendor       string     `gorm:"type:varchar(128)" json:"vendor"`             // 维修厂商（如：太鲁格）
	ContactName  string     `gorm:"type:varchar(64)" json:"contact_name"`        // 联系人
	ContactPhone string     `gorm:"type:varchar(32)" json:"contact_phone"`       // 联系电话
	SendDate     *time.Time `json:"send_date"`                                   // 寄修日期
	ReturnDate   *time.Time `json:"return_date"`                                 // 寄回日期
	Cost         float64    `gorm:"type:decimal(10,2)" json:"cost"`              // 维修金额
	Result       string     `gorm:"type:text" json:"result"`                     // 维修结果
	Status       string     `gorm:"type:varchar(32);default:'repairing'" json:"status"` // repairing(寄修中), returned(已寄回), scrapped(报废)
}

func (AssetRepair) TableName() string {
	return "asset_repairs"
}
