//go:build windows

package protection

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// 测试只写自控的 HKCU 并在结束时清理；往返/缺失断言一律经 loadAt 收在
// HKCU 内——装有生产 Agent 的开发机 HKLM 会有真策略（Load 是 HKLM
// 优先），经 Load 断言会永远对不上（2026-09-26 cmp001 装 0.2.7 后实锤）

func TestPersistLoadRoundTrip(t *testing.T) {
	p := Policy{
		Quit:      ModuleState{Enabled: true, PasswordHash: "$argon2id$v=19$m=65536,t=3,p=4$abc$def"},
		Uninstall: ModuleState{Enabled: false},
	}
	if err := persistAt(registry.CURRENT_USER, mustSerialize(t, p)); err != nil {
		t.Fatalf("persist to HKCU: %v", err)
	}
	t.Cleanup(func() {
		clearHKCUProtection(t)
	})

	got, ok := loadAt(registry.CURRENT_USER)
	if !ok {
		t.Fatal("expected persisted policy to load")
	}
	if got != p {
		t.Fatalf("round trip mismatch: %+v != %+v", got, p)
	}
}

func TestLoadUnpersistedReturnsFalse(t *testing.T) {
	clearHKCUProtection(t)
	if _, ok := loadAt(registry.CURRENT_USER); ok {
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

// clearHKCUProtection 兜底清掉 HKCU 上的 Protection 值（前次测试失败的
// 残留不阻塞本轮断言；键不存在是正常态）
func clearHKCUProtection(t *testing.T) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue(regValueProtection)
}

func mustSerialize(t *testing.T, p Policy) string {
	t.Helper()
	data, err := serialize(p)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return data
}
