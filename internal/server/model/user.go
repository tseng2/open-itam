package model

// User represents an employee or AD user.
type User struct {
	BaseModel
	CompanyID    int64  `gorm:"index;not null" json:"company_id"`
	DepartmentID *int64 `gorm:"index" json:"department_id,omitempty"`
	Username     string `gorm:"type:varchar(128);not null" json:"username"` // AD sAMAccountName
	JobNumber    string `gorm:"type:varchar(64)" json:"job_number"`         // Employee ID
	RealName     string `gorm:"type:varchar(128)" json:"real_name"`
	Email        string `gorm:"type:varchar(128)" json:"email"`
	Status       string `gorm:"type:varchar(32)" json:"status"` // e.g. "active", "inactive"
	PasswordHash string `gorm:"type:varchar(255)" json:"-"`     // Password hash, not returned in JSON
	Role         string `gorm:"type:varchar(32);default:'user'" json:"role"` // super_admin, admin, user
}
