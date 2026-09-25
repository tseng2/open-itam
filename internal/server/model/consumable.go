package model

import (
	"time"
)

// P2 耗材管理：库存物料台账 + 追加式出入库流水。
// 库存数量只经流水变更（入库/出库/调整），编辑面不可直接改库存——
// 每一笔库存变动都有流水对应，账实可追溯。流水不挂 BaseModel 软删除：
// 追加式不可变，无修改/删除面（operation_logs 先例）

// 流水类型常量：入库 / 出库（领用发放）/ 库存调整（盘盈盘亏等带符号修正）
const (
	ConsumableTxnStockIn  = "stock_in"
	ConsumableTxnStockOut = "stock_out"
	ConsumableTxnAdjust   = "adjust"
)

// ConsumableConsumableTxnTypes 合法流水类型集合（store 校验用）
var ConsumableTxnTypes = map[string]bool{
	ConsumableTxnStockIn:  true,
	ConsumableTxnStockOut: true,
	ConsumableTxnAdjust:   true,
}

type Consumable struct {
	BaseModel
	CompanyID   int64  `gorm:"index;not null" json:"company_id"`
	Name        string `gorm:"type:varchar(128);not null" json:"name"` // 名称（A4 打印纸）
	Spec        string `gorm:"type:varchar(128)" json:"spec"`           // 规格型号（70g/500张/包）
	Unit        string `gorm:"type:varchar(32)" json:"unit"`           // 计量单位（包/盒/个）
	Stock       int    `gorm:"not null;default:0" json:"stock"`         // 当前库存（只经流水变更）
	MinQuantity int    `gorm:"not null;default:0" json:"min_quantity"`  // 最低库存预警线（0 = 不预警）
	Remark      string `gorm:"type:text" json:"remark"`

	// 服务端派生字段（不落库）：库存预警 = 配置了预警线且库存触线
	LowStock bool `gorm:"-" json:"low_stock,omitempty"`
}

func (Consumable) TableName() string { return "consumables" }

// ConsumableTxn 出入库流水。Delta 是带符号的库存变动量：
// 入库恒正、出库恒负（落库为负，输入为正数量由 API 层换算）、调整任意非零。
// OperatorName 是操作人姓名快照（用户改名不影响历史展示）
type ConsumableTxn struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	CompanyID    int64     `gorm:"index;not null" json:"company_id"`
	ConsumableID int64     `gorm:"index;not null" json:"consumable_id"`
	Type         string    `gorm:"type:varchar(16);index;not null" json:"type"`
	Delta        int       `gorm:"not null" json:"delta"`
	Recipient    string    `gorm:"type:varchar(64)" json:"recipient"`    // 领用人（出库场景）
	OperatorID   int64     `gorm:"index" json:"operator_id"`             // 操作人（JWT）
	OperatorName string    `gorm:"type:varchar(64);not null" json:"operator_name"` // 操作人姓名快照
	Remark       string    `gorm:"type:varchar(255)" json:"remark"`
	CreatedAt    time.Time `gorm:"index;not null" json:"created_at"`
}

func (ConsumableTxn) TableName() string { return "consumable_txns" }
