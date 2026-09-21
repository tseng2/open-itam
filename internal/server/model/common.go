package model

import (
	"time"

	"gorm.io/gorm"
)

// BaseModel provides common fields for all database models
type BaseModel struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
	// 必须是 gorm.DeletedAt 类型：普通 *time.Time 不会触发 GORM 的软删除自动过滤，
	// 删除的记录仍会出现在查询结果里
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}
