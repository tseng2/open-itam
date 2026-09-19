package model

import "time"

// Asset 代表实物硬件资产台账（PC、笔记本、显示器、打印机等）
type Asset struct {
	BaseModel
	CompanyID    int64      `gorm:"index;not null" json:"company_id"`
	Company      Company    `gorm:"foreignKey:CompanyID" json:"company,omitempty"`
	CategoryID   int64      `gorm:"index;not null" json:"category_id"` // 1:台式机 2:笔记本 3:显示器 4:外设等
	AssetTag     string     `gorm:"type:varchar(64);uniqueIndex;not null" json:"asset_tag"` // 资产标签编码/条码
	U8OrderNo    string     `gorm:"type:varchar(64);index" json:"u8_order_no"`               // 用友U8采购订单号
	UserID       *int64     `gorm:"index" json:"user_id"`                                    // 当前领用人员ID（为空表示在库）
	User         *User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Status       int        `gorm:"default:10;index;not null" json:"status"`                 // 10-库存中, 20-使用中, 30-维修中, 40-已报废
	Brand        string     `gorm:"type:varchar(64)" json:"brand"`                           // 品牌 (联想、DELL等)
	ModelName    string     `gorm:"type:varchar(128)" json:"model"`                          // 型号规格
	SerialNumber string     `gorm:"type:varchar(128);index" json:"serial_number"`            // 硬件出厂序列号(SN)
	PurchaseDate *time.Time `json:"purchase_date"`                                           // 采购日期
	Price        float64    `gorm:"type:decimal(10,2)" json:"price"`                         // 采购单价
	Remark       string     `gorm:"type:text" json:"remark"`                                 // 备注
	Device       *Device    `gorm:"foreignKey:AssetID" json:"device,omitempty"`              // 关联绑定的动态采集设备
}

func (Asset) TableName() string {
	return "assets"
}
