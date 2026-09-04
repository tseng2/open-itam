package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir + "/agent.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HeartbeatIntervalSec != 600 || cfg.FullIntervalSec != 3600 || cfg.SpoolScanSec != 30 {
		t.Fatalf("defaults wrong: %+v", cfg)
	}
	if cfg.SpoolDir == "" {
		t.Fatal("spool dir should default to non-empty")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/agent.json"

	cfg := Default()
	cfg.ServerPrimary = "http://127.0.0.1:8443"
	cfg.ServerBackup = "https://agent.example.com"
	cfg.InstallToken = "it"
	cfg.DeviceID = "dev-x"
	cfg.DeviceToken = "dt"
	cfg.HeartbeatIntervalSec = 120
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.DeviceID != "dev-x" || loaded.DeviceToken != "dt" {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}
	if loaded.HeartbeatIntervalSec != 120 {
		t.Fatalf("custom interval not persisted: %d", loaded.HeartbeatIntervalSec)
	}
}

func TestLoadCorruptFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/agent.json"
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	if err := writeFileRaw(path, []byte("{corrupt")); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("corrupt file should not hard fail: %v", err)
	}
	if cfg.HeartbeatIntervalSec != 600 {
		t.Fatal("did not fall back to defaults")
	}
}

func TestApplyServerOverride(t *testing.T) {
	cfg := Default()
	cfg.HeartbeatIntervalSec = 600
	cfg.FullIntervalSec = 3600

	changed := cfg.ApplyServerOverride(0, 0)
	if changed || cfg.HeartbeatIntervalSec != 600 {
		t.Fatal("zero override must be ignored")
	}
	changed = cfg.ApplyServerOverride(120, 1800)
	if !changed || cfg.HeartbeatIntervalSec != 120 || cfg.FullIntervalSec != 1800 {
		t.Fatalf("override not applied: %+v", cfg)
	}
}

func TestActiveServerSelection(t *testing.T) {
	cfg := Default()
	cfg.ServerPrimary = "http://p"
	cfg.ServerBackup = "http://b"
	if cfg.ActiveServer() != "http://p" {
		t.Fatal("should use primary by default")
	}
	cfg.UsingBackup = true
	if cfg.ActiveServer() != "http://b" {
		t.Fatal("should use backup when flagged")
	}
	cfg.ServerBackup = ""
	if cfg.ActiveServer() != "http://p" {
		t.Fatal("empty backup must fall back to primary")
	}
}

func TestLoadSanitizesInvalidValues(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/agent.json"
	writeFileRaw(path, []byte(`{"heartbeat_interval_sec":-5,"full_interval_sec":0,"spool_dir":""}`))
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HeartbeatIntervalSec != 600 || cfg.FullIntervalSec != 3600 || cfg.SpoolDir == "" {
		t.Fatalf("invalid values must be reset to defaults: %+v", cfg)
	}
}

func TestImmutabilityOfDefaults(t *testing.T) {
	a := Default()
	b := Default()
	a.HeartbeatIntervalSec = 1
	if b.HeartbeatIntervalSec != 600 {
		t.Fatal("Default() must return a fresh value each time")
	}
}
