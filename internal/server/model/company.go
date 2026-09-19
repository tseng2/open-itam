package model

// Company represents a subsidiary or tenant in the group company model.
type Company struct {
	BaseModel
	Name   string `gorm:"type:varchar(128);not null;unique" json:"name"`
	Domain string `gorm:"type:varchar(128)" json:"domain"` // e.g. jg.com
	Code   string `gorm:"type:varchar(64)" json:"code"`
}
