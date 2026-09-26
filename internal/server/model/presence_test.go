package model

import (
	"testing"
	"time"
)

func presenceFixture() (time.Time, time.Duration) {
	return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), 15 * time.Minute
}

func activeDispatch(expectedReturn time.Time, isolation bool) *AssetDispatch {
	return &AssetDispatch{
		Status:           DispatchStatusActive,
		ExpectedReturnAt: expectedReturn,
		IsolationOffline: isolation,
	}
}

func TestResolvePresenceOnline(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
	})
	if got != PresenceOnline {
		t.Fatalf("expected online, got %q", got)
	}
}

// 漫游双维（2026-09-26 升级）：出口 IP 非空不再是漫游依据——
// 公司统一出口 NAT 下全部内网终端会被误判，必须叠加网络维或地理维
func TestResolvePresenceRoamingByLocalPublicIP(t *testing.T) {
	now, threshold := presenceFixture()
	// 网络维：本机直接持有公网 IP（拨号/4G/直连）
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP:   "203.0.113.7",
		LocalIP:    "203.0.113.7/32",
	})
	if got != PresenceRoaming {
		t.Fatalf("public local ip must be roaming, got %q", got)
	}
}

func TestResolvePresenceRoamingByEgressProvince(t *testing.T) {
	now, threshold := presenceFixture()
	// 地理维：本机私网（酒店/客户现场 NAT）但出口 IP 异省
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP:   "114.114.114.114", LocalIP: "192.168.1.10",
		EgressCountry: "中国", EgressProvince: "江苏省", HomeProvince: "广东省",
	})
	if got != PresenceRoaming {
		t.Fatalf("cross-province egress must be roaming, got %q", got)
	}
}

func TestResolvePresenceRoamingByOverseasEgress(t *testing.T) {
	now, threshold := presenceFixture()
	// 地理维：出口海外（人在国外，即便接入当地私网）
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP:   "8.8.8.8", LocalIP: "192.168.1.10",
		EgressCountry: "United States",
	})
	if got != PresenceRoaming {
		t.Fatalf("overseas egress must be roaming, got %q", got)
	}
}

func TestResolvePresenceCompanyNatIsOnline(t *testing.T) {
	now, threshold := presenceFixture()
	// 修复目标场景：公司统一出口 NAT（出口 IP 与公司同省）+ 本机私网 → 在线
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP:   "61.142.9.88", LocalIP: "172.20.36.7/24",
		EgressCountry: "中国", EgressProvince: "广东省", HomeProvince: "广东省",
	})
	if got != PresenceOnline {
		t.Fatalf("company NAT must be online, got %q", got)
	}
}

func TestResolvePresenceGeoUnknownDegradesToOnline(t *testing.T) {
	now, threshold := presenceFixture()
	// GeoIP 解析不出（空段/库不可用/内网保留 IP）且本机私网 → 跳过地理维度，
	// 宁漏报漫游不误报在线（无 RegionOf 注入的调用方同样落到这里）
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP:   "203.0.113.7", LocalIP: "10.1.1.5/24",
	})
	if got != PresenceOnline {
		t.Fatalf("unresolved geo must degrade to online, got %q", got)
	}
}

func TestResolvePresenceMissing(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-time.Hour),
	})
	if got != PresenceMissing {
		t.Fatalf("expected missing, got %q", got)
	}
}

func TestResolvePresenceNeverSeenIsMissing(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{Now: now, HeartbeatTimeout: threshold})
	if got != PresenceMissing {
		t.Fatalf("zero last_seen (never seen) must be missing, got %q", got)
	}
}

func TestResolvePresenceOverdueBeatsOnline(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-5 * time.Minute),
		ActiveDispatch: activeDispatch(now.Add(-time.Hour), false),
	})
	if got != PresenceOverdue {
		t.Fatalf("overdue dispatch must win even when online, got %q", got)
	}
}

func TestResolvePresenceOverdueBeatsOffline(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-time.Hour),
		ActiveDispatch: activeDispatch(now.Add(-time.Hour), true),
	})
	if got != PresenceOverdue {
		t.Fatalf("overdue dispatch must win even when offline, got %q", got)
	}
}

func TestResolvePresenceDispatchOffline(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-time.Hour),
		ActiveDispatch: activeDispatch(now.Add(24*time.Hour), true),
	})
	if got != PresenceDispatchOffline {
		t.Fatalf("isolated dispatch offline within period must be expected, got %q", got)
	}
}

func TestResolvePresenceDispatchWithoutIsolationOfflineIsMissing(t *testing.T) {
	now, threshold := presenceFixture()
	// 外派未标隔离而离线：无豁免依据，保守报疑似失联，
	// 管理员应核实或补登隔离标记
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-time.Hour),
		ActiveDispatch: activeDispatch(now.Add(24*time.Hour), false),
	})
	if got != PresenceMissing {
		t.Fatalf("dispatch offline without isolation flag must be missing, got %q", got)
	}
}

func TestResolvePresenceDispatchIsolationButOnlineShowsFactual(t *testing.T) {
	now, threshold := presenceFixture()
	// 隔离标记意味着"应离线"，设备实际在线时如实展示在线/漫游，
	// 违规联网的告警语义交给后续 A4/网卡识别，不在五态内混判
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-5 * time.Minute),
		ActiveDispatch: activeDispatch(now.Add(24*time.Hour), true),
	})
	if got != PresenceOnline {
		t.Fatalf("isolation-marked device that is online must show online, got %q", got)
	}

	// 在线的外派隔离设备接入公网（4G）照样如实展示漫游
	got = ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-5 * time.Minute),
		PublicIP:       "203.0.113.7", LocalIP: "203.0.113.7",
		ActiveDispatch: activeDispatch(now.Add(24*time.Hour), true),
	})
	if got != PresenceRoaming {
		t.Fatalf("isolation-marked device online via public ip must show roaming, got %q", got)
	}
}

func TestResolvePresenceThresholdBoundary(t *testing.T) {
	now, threshold := presenceFixture()
	// 恰好等于阈值不算离线（严格大于才判），容忍心跳抖动的边界语义
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-threshold),
	})
	if got != PresenceOnline {
		t.Fatalf("last_seen exactly at threshold must be online, got %q", got)
	}
}

func TestResolvePresenceOverdueBoundary(t *testing.T) {
	now, threshold := presenceFixture()
	// 恰好到达预计归期不算超期（严格晚于才判）
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt:     now.Add(-5 * time.Minute),
		ActiveDispatch: activeDispatch(now, false),
	})
	if got != PresenceOnline {
		t.Fatalf("expected_return exactly now must not be overdue, got %q", got)
	}
}

func TestIsPubliclyRoutedIP(t *testing.T) {
	cases := map[string]bool{
		"172.20.36.7/24": false, // 内网 + CIDR 后缀
		"10.1.1.5":       false,
		"192.168.1.10":    false,
		"203.0.113.7":     true, // 公网
		"100.64.0.1":      true, // CGN 段按公网（不在 RFC1918）
		"127.0.0.1":       false,
		"":                false,
		"garbage":         false,
		"fe80::1":         false, // IPv6 链路本地
		"fc00::abcd":      false, // IPv6 ULA 私网
		"2400:8800::1":    true,  // IPv6 全球单播
	}
	for raw, want := range cases {
		if got := isPubliclyRoutedIP(raw); got != want {
			t.Errorf("isPubliclyRoutedIP(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestResolveAssetPresenceBatch(t *testing.T) {
	now, threshold := presenceFixture()
	geo := PresenceGeo{
		HomeProvince: "广东省",
		RegionOf: func(ip string) (string, string) {
			switch ip {
			case "61.142.9.88":
				return "中国", "广东省" // 公司出口
			case "114.114.114.114":
				return "中国", "江苏省" // 异省出口
			case "8.8.8.8":
				return "United States", "California"
			}
			return "", ""
		},
	}
	assets := []Asset{
		{BaseModel: BaseModel{ID: 1}, Device: &Device{LastSeenAt: now.Add(-5 * time.Minute)}}, // 在线
		{BaseModel: BaseModel{ID: 2}, Device: &Device{LastSeenAt: now.Add(-time.Hour)}},        // 失联
		{BaseModel: BaseModel{ID: 3}}, // 无终端：跳过
		// 公司 NAT：出口同省 + 本机私网 → 在线（修复目标场景）
		{BaseModel: BaseModel{ID: 4}, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "172.20.36.7/24", PublicIP: "61.142.9.88"}},
		// 异省出口 → 漫游
		{BaseModel: BaseModel{ID: 5}, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.10", PublicIP: "114.114.114.114"}},
		// 海外出口 → 漫游
		{BaseModel: BaseModel{ID: 6}, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.11", PublicIP: "8.8.8.8"}},
	}
	dispatchByAsset := map[int64]*AssetDispatch{
		2: activeDispatch(now.Add(time.Hour), true), // 外派隔离未超期：预期内离线
	}
	ResolveAssetPresence(assets, dispatchByAsset, now, threshold, geo)

	expect := map[int64]string{
		1: PresenceOnline, 2: PresenceDispatchOffline, 3: "",
		4: PresenceOnline, 5: PresenceRoaming, 6: PresenceRoaming,
	}
	for i, a := range assets {
		if a.Presence != expect[a.ID] {
			t.Fatalf("asset %d (idx %d): expected presence %q, got %q", a.ID, i, expect[a.ID], a.Presence)
		}
	}
}

// RegionOf 未注入（nil）时只保留网络维判定，地理维整体跳过
func TestResolveAssetPresenceWithoutGeo(t *testing.T) {
	now, threshold := presenceFixture()
	assets := []Asset{
		// 出口 IP 存在但无 GeoIP：本机私网 → 在线（降级不误报）
		{BaseModel: BaseModel{ID: 1}, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "10.1.1.5", PublicIP: "203.0.113.7"}},
	}
	ResolveAssetPresence(assets, nil, now, threshold, PresenceGeo{})
	if assets[0].Presence != PresenceOnline {
		t.Fatalf("without geo, private local ip must stay online, got %q", assets[0].Presence)
	}
}
