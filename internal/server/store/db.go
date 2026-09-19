package store

import (
	"fmt"
	"log"

	"itagent/internal/server/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDB 初始化数据库连接并自动迁移模型
// 默认支持 SQLite，未来可通过配置灵活切换 PostgreSQL / MySQL
func InitDB(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		dsn = "itagent.db"
	}

	var err error
	DB, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	// 自动建表与表结构迁移
	err = DB.AutoMigrate(
		&model.Company{},
		&model.Department{},
		&model.User{},
		&model.Asset{},
		&model.Device{},
		&model.AssetEvent{},
	)
	if err != nil {
		return nil, fmt.Errorf("auto migration failed: %w", err)
	}

	log.Printf("[DB] Database initialized and migrated successfully using DSN: %s", dsn)
	return DB, nil
}
