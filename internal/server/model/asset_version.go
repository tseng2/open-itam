package model

// AssetVersion 记录资产硬件基线的历史版本快照
type AssetVersion struct {
	BaseModel
	AssetID          int64  `gorm:"index;not null" json:"asset_id"` // 关联资产ID
	Asset            Asset  `gorm:"foreignKey:AssetID" json:"asset,omitempty"`
	Version          int    `gorm:"not null" json:"version"`                       // 版本号
	HardwareSnapshot string `gorm:"type:text" json:"hardware_snapshot"`            // 硬件快照 (JSON格式，存储完整的CPU/内存/磁盘等信息)
	ChangeReason     string `gorm:"type:varchar(255)" json:"change_reason"`        // 变更原因
	ApprovedByID     *int64 `gorm:"index" json:"approved_by_id"`                   // 审核通过的管理员ID
	ApprovedBy       *User  `gorm:"foreignKey:ApprovedByID" json:"approved_by,omitempty"`
}

func (AssetVersion) TableName() string {
	return "asset_versions"
}
