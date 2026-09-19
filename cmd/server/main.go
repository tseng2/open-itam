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
	DBPath              string `json:"db_path"`
	InstallToken        string `json:"install_token"`
	AdminToken          string `json:"admin_token"`
	DefaultHeartbeatSec int    `json:"default_heartbeat_sec"`
	DefaultFullSec      int    `json:"default_full_sec"`
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

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}
	st, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := store.InitDB(cfg.DBPath); err != nil {
		log.Fatalf("init gorm store: %v", err)
	}

	h := api.NewHandler(st, api.Config{
		InstallToken:        cfg.InstallToken,
		AdminToken:          cfg.AdminToken,
		DefaultHeartbeatSec: cfg.DefaultHeartbeatSec,
		DefaultFullSec:      cfg.DefaultFullSec,
	})

	root := ui.Wrap(h)
	log.Printf("itagent server listening on %s, db=%s", cfg.Listen, cfg.DBPath)
	if err := http.ListenAndServe(cfg.Listen, root); err != nil {
		log.Fatal(err)
	}
}
