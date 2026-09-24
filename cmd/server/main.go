package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"itagent/internal/server/api"
	"itagent/internal/server/store"
	"itagent/internal/server/ui"
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
	OfflineThresholdSec int    `json:"offline_threshold_sec"`
	// Agent 更新清单（version/file/sha256），默认 data/updates/manifest.json
	UpdateManifest string `json:"update_manifest"`
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
	log.Printf("itagent server listening on %s, db=%s", cfg.Listen, cfg.DBPath)
	if err := http.ListenAndServe(cfg.Listen, root); err != nil {
		log.Fatal(err)
	}
}
