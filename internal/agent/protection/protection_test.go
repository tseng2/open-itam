package protection

import (
	"encoding/json"
	"testing"
)

func TestParseServerConfigFull(t *testing.T) {
	body := []byte(`{
		"code": 0,
		"heartbeat_interval_sec": 600,
		"full_interval_sec": 3600,
		"quit_protection": {"enabled": true, "password_hash": "$argon2id$v=19$m=65536,t=3,p=4$abc$def"},
		"uninstall_protection": {"enabled": false, "password_hash": ""}
	}`)
	p, err := ParseServerConfig(body)
	if err != nil {
		t.Fatalf("parse server config: %v", err)
	}
	if !p.Quit.Enabled || p.Quit.PasswordHash == "" {
		t.Fatalf("expected quit module enabled with hash, got %+v", p.Quit)
	}
	if p.Uninstall.Enabled {
		t.Fatal("expected uninstall module disabled")
	}
}

func TestParseServerConfigMissingFieldsDefaultsToDisabled(t *testing.T) {
	body := []byte(`{"code": 0, "heartbeat_interval_sec": 600}`)
	p, err := ParseServerConfig(body)
	if err != nil {
		t.Fatalf("parse config without protection fields: %v", err)
	}
	if p.Quit.Enabled || p.Uninstall.Enabled {
		t.Fatalf("missing fields must default to disabled, got %+v", p)
	}
}

func TestParseServerConfigInvalidJSON(t *testing.T) {
	if _, err := ParseServerConfig([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestUnmarshalRoundTrip(t *testing.T) {
	p := Policy{
		Quit:      ModuleState{Enabled: true, PasswordHash: "$argon2id$hash"},
		Uninstall: ModuleState{Enabled: false},
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Policy
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != p {
		t.Fatalf("round trip mismatch: %+v != %+v", back, p)
	}
}
