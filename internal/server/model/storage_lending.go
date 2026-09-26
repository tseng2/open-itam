package model

import "time"

// StorageLending 对应移动存储领用表（U 盘、移动硬盘等领用/归还登记）
type StorageLending struct {
	BaseModel
	CompanyID  int64      `gorm:"index;not null" json:"company_id"`            // 归属公司
	Company    Company    `gorm:"foreignKey:CompanyID" json:"company,omitempty"`
	Department string     `gorm:"type:varchar(128);index" json:"department"`   // 领用部门
	Borrower   string     `gorm:"type:varchar(64);index" json:"borrower"`      // 领用人
	BorrowDate *time.Time `json:"borrow_date"`                                 // 领用日期
	Brand      string     `gorm:"type:varchar(64)" json:"brand"`               // 品牌
	Spec       string     `gorm:"type:varchar(64)" json:"spec"`                // 规格（容量等）
	DeviceCode string     `gorm:"type:varchar(64);index" json:"device_code"`   // 设备编码
	Quantity   int        `gorm:"default:1" json:"quantity"`                   // 领用数量
	ReturnDate *time.Time `json:"return_date"`                                 // 归还日期
	ReturnQty  int        `json:"return_qty"`                                  // 归还数量
	SecCertified bool     `gorm:"default:false" json:"sec_certified"`          // 加密系统认证（通用标记，产品名不落码）
	Remark     string     `gorm:"type:text" json:"remark"`                     // 备注
}

func (StorageLending) TableName() string {
	return "storage_lendings"
}
