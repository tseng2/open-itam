package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"itagent/internal/server/api"
	"itagent/internal/server/api/middleware"
	v1 "itagent/internal/server/api/v1"
	"itagent/internal/server/depreciation"
	"itagent/internal/server/licensealert"
	"itagent/internal/server/store"
	"itagent/internal/server/ui"
	"itagent/internal/server/webhook"
)

type serverConfig struct {
	Listen              string `json:"listen"`
	DBType              string `json:"db_type"`
	DBPath              string `json:"db_path"`
	InstallToken        string `json:"install_token"`
	AdminToken          string `json:"admin_token"`
	DefaultHeartbeatSec int    `json:"default_heartbeat_sec"`
	DefaultFullSec      int    `json:"default_full_sec"`
	// A2 失联语义分层：资产联系状态的离线判定阈值（秒），0 = 服务端默认 15 分钟
	OfflineThresholdSec int `json:"offline_threshold_sec"`
	// Agent 更新清单（version/file/sha256），默认 data/updates/manifest.json
	UpdateManifest string `json:"update_manifest"`
	// JWT 签名密钥（任务 B 配置化）：未配置回落内置默认并启动告警；
	// 环境变量 ITAGENT_JWT_SECRET 兜底。secret 变更后存量 token
	// 全失效（401 → 前端跳登录）属预期
	JWTSecret string `json:"jwt_secret"`
	// 软件许可到期提醒窗口（天）：0 = 默认 30 天（licensealert 引擎）
	LicenseExpiringDays int `json:"license_expiring_days"`
}

func main() {
	configPath := flag.String("config", "configs/server.json", "path to server config")
	flag.Parse()

	data, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	var cfg serverConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8443"
	}
	if cfg.DBType == "" {
		cfg.DBType = "sqlite"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "data/server.db"
	}
	if cfg.InstallToken == "" || cfg.AdminToken == "" {
		log.Fatal("install_token and admin_token must be set in config")
	}
	if cfg.DefaultHeartbeatSec == 0 {
		cfg.DefaultHeartbeatSec = 600
	}
	if cfg.DefaultFullSec == 0 {
		cfg.DefaultFullSec = 3600
	}
	if cfg.UpdateManifest == "" {
		cfg.UpdateManifest = "data/updates/manifest.json"
	}

	// JWT secret 配置化：server.json 优先，环境变量兜底，双缺省回落
	// 内置默认（向后兼容）+ 启动告警；生产必须显式配置
	jwtSecret := cfg.JWTSecret
	if jwtSecret == "" {
		jwtSecret = os.Getenv("ITAGENT_JWT_SECRET")
	}
	if jwtSecret == "" {
		log.Printf("[warn] jwt_secret not configured, falling back to built-in default; set jwt_secret in %s or ITAGENT_JW_SECRET env to override", *configPath)
	} else {
		middleware.SetJWTSecret(jwtSecret)
	}

	if cfg.DBType == "sqlite" {
		if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
			log.Fatalf("create db dir: %v", err)
		}
	}

	db, err := store.InitDB(cfg.DBType, cfg.DBPath)
	if err != nil {
		log.Fatalf("init gorm store: %v", err)
	}

	st := store.NewGormStore(db)

	h := api.NewHandler(st, api.Config{
		InstallToken:        cfg.InstallToken,
		AdminToken:          cfg.AdminToken,
		DefaultHeartbeatSec: cfg.DefaultHeartbeatSec,
		DefaultFullSec:      cfg.DefaultFullSec,
		OfflineThresholdSec: cfg.OfflineThresholdSec,
		UpdateManifest:      cfg.UpdateManifest,
	})

	root := ui.Wrap(h)

	// A4 告警引擎（双通道出站）：WebHook（配置开关控制）+ 站内信
	//（注入 v1 闭包扇出公司管理员，独立于 WebHook 开关）。
	// 单进程 goroutine + Ticker 定时扫描，无需分布式锁；
	// ctx 随进程退出自动取消
	alertEngine := webhook.NewEngine(db, st, api.ResolveOfflineThreshold(cfg.OfflineThresholdSec), v1.NewAlertNotifier())
	engineCtx, stopEngine := context.WithCancel(context.Background())
	defer stopEngine()
	go alertEngine.Run(engineCtx, webhook.DefaultScanInterval)

	// P0-β 折旧引擎：定时按规则刷资产净值（默认每小时，净值只在整月边界
	// 变化）；规则/台账编辑有手动重算入口，无需更密的扫描
	depEngine := depreciation.NewEngine(db)
	go depEngine.Run(engineCtx, depreciation.DefaultScanInterval)

	// 软件许可到期提醒引擎：每小时扫描 ExpiringDays 窗口内的许可，
	// 提醒公司管理员（注入 v1 闭包扇出；窗口去重见 licensealert 契约）
	licEngine := licensealert.NewEngine(db, st, cfg.LicenseExpiringDays, v1.NewLicenseExpiringNotifier())
	go licEngine.Run(engineCtx, licensealert.DefaultScanInterval)

	log.Printf("itagent server listening on %s, db=%s", cfg.Listen, cfg.DBPath)
	if err := http.ListenAndServe(cfg.Listen, root); err != nil {
		log.Fatal(err)
	}
}
