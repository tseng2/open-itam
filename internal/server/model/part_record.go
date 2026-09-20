package model

import "time"

// 配件出入方向常量，避免业务代码散落字面量
const (
	PartDirectionIn  = "in"
	PartDirectionOut = "out"
)

// PartRecord 对应配件记录表（IT 配件出入库流水，独立于单个资产的事件）
type PartRecord struct {
	BaseModel
	CompanyID      int64      `gorm:"index;not null" json:"company_id"`           // 归属公司
	Direction      string     `gorm:"type:varchar(8);index;not null" json:"direction"` // in(入库) / out(出库)
	OperatedAt     *time.Time `json:"operated_at"`                                // 操作时间（业务发生时间，区别于创建时间）
	PartType       string     `gorm:"type:varchar(32);index" json:"part_type"`    // 物品类型（内存/硬盘/键鼠等）
	PartName       string     `gorm:"type:varchar(128)" json:"part_name"`         // 物品名称
	PartModel      string     `gorm:"type:varchar(128)" json:"part_model"`        // 规格型号
	Brand          string     `gorm:"type:varchar(64)" json:"brand"`              // 品牌
	Quantity       int        `gorm:"default:1" json:"quantity"`                  // 数量
	Unit           string     `gorm:"type:varchar(16)" json:"unit"`               // 单位（条/个/块）
	LockerLocation string     `gorm:"type:varchar(64)" json:"locker_location"`    // IT 储物柜位置
	Purpose        string     `gorm:"type:text" json:"purpose"`                   // 用途
	OANumber       string     `gorm:"type:varchar(64)" json:"oa_number"`          // OA 单号
	Location       string     `gorm:"type:varchar(128)" json:"location"`          // 存放位置
	AssetTag       string     `gorm:"type:varchar(64);index" json:"asset_tag"`    // 关联固定资产编号
	OperatorID     *int64     `gorm:"index" json:"operator_id"`                   // 操作人
	Operator       *User      `gorm:"foreignKey:OperatorID" json:"operator,omitempty"`
}

func (PartRecord) TableName() string {
	return "part_records"
}
