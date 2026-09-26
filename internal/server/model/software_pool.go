package model

import "time"

// 阶段三软件合规比对：受控软件池（公司维度商业软件白名单）+ 终端软件
// 清单结构化快照。口径见 docs/implementation_plan.md「软件与授权管理」：
// Agent 采集的商业软件与受控池比对，产出合规报表（识别盗版与超用）。

// SoftwarePool 受控软件池：入池即受控。Name 是匹配键——终端软件名
// 精确命中优先，其次按「采集名包含池名」模糊命中（注册表 DisplayName
// 变体多，池里写「Microsoft 365」即可命中「Microsoft 365 Apps for
// enterprise」）。LicenseID 可空挂接 licenses（席位来源；NULL = 不限
// 席位，不做超用判定）
type SoftwarePool struct {
	BaseModel
	CompanyID int64     `gorm:"index;not null" json:"company_id"`
	Name      string    `gorm:"type:varchar(128);not null" json:"name"`
	Vendor    string    `gorm:"type:varchar(128)" json:"vendor"`
	Category  string    `gorm:"type:varchar(64)" json:"category"`
	LicenseID *int64    `gorm:"index" json:"license_id"`
	License   *License  `gorm:"foreignKey:LicenseID" json:"license,omitempty"`
	Remark    string    `gorm:"type:text" json:"remark"`

	// 富化字段（不落库）：列表/详情带挂接许可名，supplier_name 先例
	LicenseName string `gorm:"-" json:"license_name,omitempty"`
}

func (SoftwarePool) TableName() string { return "software_pools" }

// DeviceSoftware 终端软件清单结构化快照：full 上报（每小时）时每终端
// 全量覆盖——卸载即消失，无软删除语义。公司归属取自绑定的台账资产
//（agent_devices.asset_id → assets.company_id）；**未绑定资产的终端
// 不参与合规统计**，绑定后下一次 full 上报自动纳入
type DeviceSoftware struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CompanyID   int64     `gorm:"index:idx_device_software_company_name,priority:1;not null" json:"company_id"`
	DeviceID    string    `gorm:"type:varchar(64);index;not null" json:"device_id"`
	AssetID     int64     `gorm:"index;not null" json:"asset_id"`
	Name        string    `gorm:"type:varchar(256);index:idx_device_software_company_name,priority:2;not null" json:"name"`
	Version     string    `gorm:"type:varchar(64)" json:"version"`
	InstallPath string    `gorm:"type:varchar(512)" json:"install_path"`
	SeenAt      time.Time `json:"seen_at"` // 最近一次 full 上报时间
}

func (DeviceSoftware) TableName() string { return "device_software" }
