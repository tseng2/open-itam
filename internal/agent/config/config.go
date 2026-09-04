package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	ServerPrimary        string `json:"server_primary"`
	ServerBackup         string `json:"server_backup"`
	InstallToken         string `json:"install_token"`
	DeviceID             string `json:"device_id"`
	DeviceToken          string `json:"device_token"`
	AgentVersion         string `json:"agent_version"`
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec"`
	FullIntervalSec      int    `json:"full_interval_sec"`
	SpoolScanSec         int    `json:"spool_scan_sec"`
	FailoverProbeSec     int    `json:"failover_probe_sec"`
	SpoolDir             string `json:"spool_dir"`
	UsingBackup          bool   `json:"using_backup"`
}

func Default() Config {
	return Config{
		HeartbeatIntervalSec: 600,
		FullIntervalSec:      3600,
		SpoolScanSec:         30,
		FailoverProbeSec:     1800,
		SpoolDir:             "data/spool",
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), nil
	}
	if cfg.HeartbeatIntervalSec <= 0 {
		cfg.HeartbeatIntervalSec = 600
	}
	if cfg.FullIntervalSec <= 0 {
		cfg.FullIntervalSec = 3600
	}
	if cfg.SpoolScanSec <= 0 {
		cfg.SpoolScanSec = 30
	}
	if cfg.FailoverProbeSec <= 0 {
		cfg.FailoverProbeSec = 1800
	}
	if cfg.SpoolDir == "" {
		cfg.SpoolDir = "data/spool"
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) ApplyServerOverride(nextHeartbeatSec, nextFullSec int) bool {
	changed := false
	if nextHeartbeatSec > 0 && nextHeartbeatSec != c.HeartbeatIntervalSec {
		c.HeartbeatIntervalSec = nextHeartbeatSec
		changed = true
	}
	if nextFullSec > 0 && nextFullSec != c.FullIntervalSec {
		c.FullIntervalSec = nextFullSec
		changed = true
	}
	return changed
}

func (c *Config) ActiveServer() string {
	if c.UsingBackup && c.ServerBackup != "" {
		return c.ServerBackup
	}
	return c.ServerPrimary
}

func writeFileRaw(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
