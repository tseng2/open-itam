package model

import "time"

// 资产状态段位（此前仅散落为裸数字；P0-β 盘点圈定范围需排除已报废，借此常量化收口）
const (
	AssetStatusStock    = 10 // 库存中
	AssetStatusInUse    = 20 // 使用中
	AssetStatusRepair   = 30 // 维修中
	AssetStatusScrapped = 40 // 已报废
)

// Asset 代表实物硬件资产台账（PC、笔记本、显示器、打印机等）
type Asset struct {
	BaseModel
	CompanyID      int64      `gorm:"index;not null" json:"company_id"`
	Company        Company    `gorm:"foreignKey:CompanyID" json:"company,omitempty"`
	CenterName     string     `gorm:"type:varchar(64);index" json:"center_name"`              // 中心（如：项目中心、研发中心）
	DepartmentName string     `gorm:"type:varchar(128);index" json:"department_name"`         // 部门（完整部门路径）
	DepartmentSub  string     `gorm:"type:varchar(64)" json:"department_sub"`                 // 部门 2
	Location       string     `gorm:"type:varchar(128)" json:"location"`                       // 主要存放位置或用途
	ManagerName    string     `gorm:"type:varchar(64)" json:"manager_name"`                   // 资产负责人

	CategoryID     int64      `gorm:"index;not null" json:"category_id"`                      // 1:台式机 2:笔记本 3:显示器 4:外设等
	CategoryName   string     `gorm:"type:varchar(32)" json:"category_name"`                  // 类别名称 (笔记本/台式机/显示器等)
	AssetTag       string     `gorm:"type:varchar(64);uniqueIndex;not null" json:"asset_tag"` // 固定资产编码/条码
	U8OrderNo      string     `gorm:"type:varchar(64);index" json:"u8_order_no"`              // 用友U8采购订单号
	CurrentVersion int        `gorm:"default:1;not null" json:"current_version"`              // 当前硬件基线版本号
	UserID         *int64     `gorm:"index" json:"user_id"`                                   // 当前领用人员ID（为空表示在库）
	User           *User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Status         int        `gorm:"default:10;index;not null" json:"status"`                // 10-库存中, 20-使用中, 30-维修中, 40-已报废

	// 联系状态（阶段五 A2 失联语义分层）：服务端按 外派登记 × LastSeenAt × 心跳阈值
	// 实时计算，不落库（gorm:"-"）；无 Agent 终端留空。五态定义见 presence.go
	Presence       string     `gorm:"-" json:"presence,omitempty"`

	Brand          string     `gorm:"type:varchar(64)" json:"brand"`                          // 品牌 (联想、DELL、苹果等)
	ModelName      string     `gorm:"type:varchar(128)" json:"model"`                         // 型号规格
	SerialNumber   string     `gorm:"type:varchar(128);index" json:"serial_number"`           // 硬件出厂序列号(SN/S/N码)

	// 账面硬件规格：人工维护的台账数据，覆盖无 Agent 终端（Mac、显示器等）；
	// 有 Agent 时展示以在线采集为准，账面值仅作建账基线，Agent 只在字段为空时回填
	CPUName        string     `gorm:"type:varchar(128)" json:"cpu_name"`                      // CPU 名称
	MemorySize     string     `gorm:"type:varchar(32)" json:"memory_size"`                    // 内存 (如: 16G)
	MainDisk       string     `gorm:"type:varchar(64)" json:"main_disk"`                      // 主硬盘 (如: 512G SSD)
	SecondaryDisk  string     `gorm:"type:varchar(64)" json:"secondary_disk"`                 // 从硬盘
	GPUName        string     `gorm:"type:varchar(128)" json:"gpu_name"`                      // 显卡名称
	MACAddress     string     `gorm:"type:varchar(64)" json:"mac_address"`                    // 网卡 MAC 地址

	PurchaseDate   *time.Time `json:"purchase_date"`                                          // 购入时间
	Acceptor       string     `gorm:"type:varchar(64)" json:"acceptor"`                       // 验收人
	WarrantyPeriod string     `gorm:"type:varchar(64)" json:"warranty_period"`                // 保修期
	OriginalPrice  float64    `gorm:"type:decimal(10,2)" json:"original_price"`               // 原值（不含税）
	NetValue       float64    `gorm:"type:decimal(10,2)" json:"net_value"`                    // 净值
	DepreciationID *int64     `gorm:"index" json:"depreciation_id"`                            // 关联折旧规则；为空不参与自动折旧（P0-β）

	// 财务维度（列管资产支持，P0-β）：off_book 与运营状态正交——
	// 折旧完且财务销账的资产转为"列管"继续给员工使用，报废变卖才离场。
	// 展示层派生"列管"标签（off_book && 未报废），严禁塞进 Status 状态机
	OffBook   bool       `gorm:"default:false;index" json:"off_book"` // 财务销账标记（账销物留）
	OffBookAt *time.Time `json:"off_book_at"`                        // 销账日期

	SecEncrypted   bool       `gorm:"default:false" json:"sec_encrypted"`                     // 加密软件管理（是否绿盾纳管）
	Remark         string     `gorm:"type:text" json:"remark"`                                // 备注
	Device         *Device    `gorm:"foreignKey:AssetID" json:"device,omitempty"`             // 关联绑定的动态采集设备
}

func (Asset) TableName() string {
	return "assets"
}
