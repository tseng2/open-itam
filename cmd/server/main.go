package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"itagent/internal/server/api"
	"itagent/internal/server/api/middleware"
	v1 "itagent/internal/server/api/v1"
	"itagent/internal/server/depreciation"
	"itagent/internal/server/licensealert"
	"itagent/internal/server/softwareaudit"
	"itagent/internal/server/store"
	"itagent/internal/server/ui"
	"itagent/internal/server/webhook"
)

type serverConfig struct {
	Listen              string `json:"listen"`
	DBType              string `json:"db_type"`
	DBPath              string `json:"db_path"`
	InstallToken string `json:"install_token"`
	AdminToken   string `json:"admin_token"`
	// Agent 更新清单（version/file/sha256），默认 data/updates/manifest.json
	UpdateManifest string `json:"update_manifest"`
	// JWT 签名密钥（任务 B 配置化）：未配置回落内置默认并启动告警；
	// 环境变量 ITAGENT_JWT_SECRET 兜底。secret 变更后存量 token
	// 全失效（401 → 前端跳登录）属预期
	JWTSecret string `json:"jwt_secret"`
	// 软件许可到期提醒窗口（天）：0 = 默认 30 天（licensealert 引擎）
	LicenseExpiringDays int `json:"license_expiring_days"`
	// 软件超用提醒冷却窗口（小时）：0 = 默认 24 小时（softwareaudit
	// 合规引擎——同一池项冷却窗内只投一次提醒）
	SoftwareOveruseCooldownHours int `json:"software_overuse_cooldown_hours"`
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
		InstallToken:  cfg.InstallToken,
		AdminToken:    cfg.AdminToken,
		UpdateManifest: cfg.UpdateManifest,
	})

	root := ui.Wrap(h)

	// A4 告警引擎（双通道出站）：WebHook（配置开关控制）+ 站内信
	//（注入 v1 闭包扇出公司管理员，独立于 WebHook 开关）。
	// 单进程 goroutine + Ticker 定时扫描，无需分布式锁；
	// 失联阈值与漫游地理基准经闭包实时读 agent_settings（设置页保存即生效）；
	// ctx 随进程退出自动取消
	alertEngine := webhook.NewEngine(db, st,
		v1.EffectivePresenceTimeout, v1.PresenceGeoFor, v1.NewAlertNotifier())
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

	// 阶段三软件合规引擎：每小时比对受控池 × 终端软件安装，超用池项
	// 经冷却去重提醒公司管理员（注入 v1 闭包扇出；未受控清单只进报表
	// 不投通知，口径见契约文档）
	swEngine := softwareaudit.NewEngine(db, st,
		time.Duration(cfg.SoftwareOveruseCooldownHours)*time.Hour, v1.NewSoftwareOveruseNotifier())
	go swEngine.Run(engineCtx, softwareaudit.DefaultScanInterval)

	log.Printf("itagent server listening on %s, db=%s", cfg.Listen, cfg.DBPath)
	if err := http.ListenAndServe(cfg.Listen, root); err != nil {
		log.Fatal(err)
	}
}
