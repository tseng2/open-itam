//go:build windows

package protection

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// TestPersistLoadRoundTrip 用 HKCU 真实注册表验证策略持久化往返；
// HKLM 需要管理员权限且可能装有生产 Agent，测试只写 HKCU 并在结束时清理
func TestPersistLoadRoundTrip(t *testing.T) {
	p := Policy{
		Quit:      ModuleState{Enabled: true, PasswordHash: "$argon2id$v=19$m=65536,t=3,p=4$abc$def"},
		Uninstall: ModuleState{Enabled: false},
	}
	if err := persistAt(registry.CURRENT_USER, mustSerialize(t, p)); err != nil {
		t.Fatalf("persist to HKCU: %v", err)
	}
	t.Cleanup(func() {
		k, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.SET_VALUE)
		if err != nil {
			return
		}
		defer k.Close()
		_ = k.DeleteValue(regValueProtection)
	})

	got, ok := Load()
	if !ok {
		t.Fatal("expected persisted policy to load")
	}
	if got != p {
		t.Fatalf("round trip mismatch: %+v != %+v", got, p)
	}
}

func TestLoadUnpersistedReturnsFalse(t *testing.T) {
	if _, ok := Load(); ok {
		t.Fatal("expected no policy when registry value is absent or unparsable")
	}
}

func TestParsePersistedInvalidJSON(t *testing.T) {
	if _, ok := parsePersisted("not json"); ok {
		t.Fatal("expected invalid persisted json to be treated as absent")
	}
	if _, ok := parsePersisted(""); ok {
		t.Fatal("expected empty persisted value to be treated as absent")
	}
}

func mustSerialize(t *testing.T, p Policy) string {
	t.Helper()
	data, err := serialize(p)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return data
}
