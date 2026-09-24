package store

import (
	"fmt"
	"log"

	"itagent/internal/server/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDB 初始化数据库连接并自动迁移模型
// 支持 SQLite 和 MySQL
func InitDB(dbType, dsn string) (*gorm.DB, error) {
	if dbType == "" {
		dbType = "sqlite"
	}
	if dsn == "" {
		if dbType == "sqlite" {
			dsn = "data/server.db"
		} else {
			return nil, fmt.Errorf("dsn is required for mysql")
		}
	}

	var dialector gorm.Dialector
	if dbType == "mysql" {
		dialector = mysql.Open(dsn)
	} else {
		dialector = sqlite.Open(dsn)
	}

	var err error
	DB, err = gorm.Open(dialector, &gorm.Config{
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
		&model.AssetRepair{},
		&model.AssetVersion{},
		&model.StorageLending{},
		&model.PartRecord{},
		&model.AgentDevice{},
		&model.AgentReport{},
		&model.AgentSnapshot{},
		&model.AgentChangeEvent{},
		&model.ProtectionModule{},
		&model.UninstallCode{},
		&model.AssetDispatch{},
		&model.WebhookAlertConfig{},
		&model.WebhookAlertState{},
		&model.Stocktake{},
		&model.StocktakeItem{},
		&model.AssetRequest{},
	)
	if err != nil {
		return nil, fmt.Errorf("auto migration failed: %w", err)
	}

	log.Printf("[DB] Database initialized and migrated successfully using %s", dbType)
	return DB, nil
}
