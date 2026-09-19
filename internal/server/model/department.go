package model

// Department represents an organizational unit within a company.
type Department struct {
	BaseModel
	CompanyID int64  `gorm:"index;not null" json:"company_id"`
	ParentID  *int64 `gorm:"index" json:"parent_id,omitempty"`
	Name      string `gorm:"type:varchar(128);not null" json:"name"`
}
