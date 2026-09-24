package model

// DepreciationRule 折旧规则（阶段五 P0-β）：公司维度实体，资产经
// Asset.DepreciationID 关联后由折旧引擎定时刷净值。
// Stages 为 JSON 数组 [{period, unit, ratio}]，按时间顺序分段累计折旧比例，
// 段内按整月线性折算；为空时按 Months 总月数直线折旧；
// FloorType/FloorVal 残值下限兜底（amount 绝对额 / percent 残值率）。
// 计算与校验核心在 internal/server/depreciation 包，禁止在本模型外复制口径
type DepreciationRule struct {
	BaseModel
	CompanyID int64   `gorm:"index;not null" json:"company_id"`
	Company   Company `gorm:"foreignKey:CompanyID" json:"company,omitempty"`
	Name      string  `gorm:"type:varchar(128);not null" json:"name"`                        // 规则名称
	Months    int     `gorm:"not null" json:"months"`                                       // 总折旧月数：直线折旧依据 + 展示校验；配置阶梯时必须与阶梯覆盖月数一致
	FloorType string  `gorm:"type:varchar(20);not null;default:'percent'" json:"floor_type"` // amount / percent
	FloorVal  float64 `gorm:"type:decimal(10,2);not null;default:0" json:"floor_val"`       // 残值金额或残值率
	Stages    string  `gorm:"type:text" json:"stages"`                   // 分段折旧规则 JSON，空串 = 直线折旧
	Enabled   bool    `gorm:"index" json:"enabled"`                       // 停用后引擎冻结其名下资产净值刷新；缺省=启用由 API 层解析。
	// 注意不能加 default:true 标签：bool 零值 false 是合法值（停用），
	// 带 default 标签时 GORM Create 会把零值替换成列默认值，停用规则永远建不出来
	Remark    string  `gorm:"type:text" json:"remark"`
}

func (DepreciationRule) TableName() string { return "depreciations" }
