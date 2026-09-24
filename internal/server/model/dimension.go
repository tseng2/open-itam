package model

// P1 维度治理：厂商 / 供应商 / 位置库 / 型号库四张维表。
// 目标是消灭台账里的自由文本维度（Brand / ModelName / Location 外键化，
// 供应商为新增维度），统一口径便于筛选与报表。
// 原自由文本列保留作 Excel 导出/标签 PDF 的落库快照：挂接外键时由
// 服务端把维度名写回快照列，读面（列表/详情/导出）再按外键富化覆盖，
// 维度重命名可传播且导出闭环不受影响。
// 名称同公司唯一（store 层校验，软删除记录不占名）；维表被资产/型号/
// 子位置引用时的删除拦截在 API 层完成（需查 assets 表，SQLiteStore
// 测试库无该表）。

// Manufacturer 厂商：资产 Brand 自由文本 → 外键
type Manufacturer struct {
	BaseModel
	CompanyID int64  `gorm:"index;not null" json:"company_id"`
	Name      string `gorm:"type:varchar(64);not null" json:"name"` // 厂商名（联想 / DELL / 苹果等）
	Remark    string `gorm:"type:text" json:"remark"`
}

func (Manufacturer) TableName() string { return "manufacturers" }

// Supplier 供应商：采购来源治理（资产 SupplierID 外键）
type Supplier struct {
	BaseModel
	CompanyID   int64  `gorm:"index;not null" json:"company_id"`
	Name        string `gorm:"type:varchar(128);not null" json:"name"` // 供应商名
	ContactName string `gorm:"type:varchar(64)" json:"contact_name"`   // 联系人
	Phone       string `gorm:"type:varchar(32)" json:"phone"`
	Remark      string `gorm:"type:text" json:"remark"`
}

func (Supplier) TableName() string { return "suppliers" }

// Location 位置库：资产 Location 自由文本 → 外键；树形结构（ParentID 空 = 顶级）
type Location struct {
	BaseModel
	CompanyID int64  `gorm:"index;not null" json:"company_id"`
	Name      string `gorm:"type:varchar(128);not null" json:"name"`
	ParentID  *int64 `gorm:"index" json:"parent_id"` // 父位置；NULL = 顶级
	Remark    string `gorm:"type:text" json:"remark"`

	// 展示富化（API 层批量填充，不落库）
	ParentName string `gorm:"-" json:"parent_name,omitempty"`
}

func (Location) TableName() string { return "locations" }

// AssetModel 型号库：资产 ModelName 自由文本 → 外键。
// 型号可预挂折旧规则：建账选择型号且未显式指定折旧规则时自动继承，
// 让同型号资产折旧口径天然一致；EOLMonths 供报表/换机提醒（0 = 不限）
type AssetModel struct {
	BaseModel
	CompanyID      int64  `gorm:"index;not null" json:"company_id"`
	Name           string `gorm:"type:varchar(128);not null" json:"name"` // 型号规格（ThinkPad X1 Carbon Gen 11 等）
	CategoryID     int64  `json:"category_id"`                             // 适用类别（复用 AssetCategory* 段位；0 = 不限）
	ManufacturerID *int64 `gorm:"index" json:"manufacturer_id"`            // 厂商外键；NULL = 未指定
	DepreciationID *int64 `gorm:"index" json:"depreciation_id"`            // 预挂折旧规则；NULL = 无
	EOLMonths      int    `json:"eol_months"`                              // EOL 寿命月数；0 = 不限
	Remark         string `gorm:"type:text" json:"remark"`

	// 展示富化（API 层批量填充，不落库）
	ManufacturerName string `gorm:"-" json:"manufacturer_name,omitempty"`
	DepreciationName string `gorm:"-" json:"depreciation_name,omitempty"`
}

func (AssetModel) TableName() string { return "asset_models" }
