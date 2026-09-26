package model

import (
	"strings"
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

// 地理维·异省：东莞公司（基准 广东省|东莞市）出口在苏州 → 漫游
func TestResolvePresenceRoamingByCrossProvinceEgress(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP:     "114.114.114.114", LocalIP: "192.168.1.10",
		EgressCountry: "中国", EgressRegion: "江苏省|南京市", HomeRegion: "广东省|东莞市",
	})
	if got != PresenceRoaming {
		t.Fatalf("cross-province egress must be roaming, got %q", got)
	}
}

// 地理维·同省异市（2026-09-26 市级升级的业务规则）：东莞公司的电脑
// 跑到广州（同省不同市）也算漫游——省级基准时代整省出差都不算的缺口收口
func TestResolvePresenceRoamingBySameProvinceDiffCity(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP:     "203.0.113.7", LocalIP: "192.168.1.10",
		EgressCountry: "中国", EgressRegion: "广东省|广州市", HomeRegion: "广东省|东莞市",
	})
	if got != PresenceRoaming {
		t.Fatalf("same-province diff-city egress must be roaming, got %q", got)
	}
}

func TestResolvePresenceRoamingByOverseasEgress(t *testing.T) {
	now, threshold := presenceFixture()
	// 地理维：出口海外（人在国外，即便接入当地私网）
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP: "8.8.8.8", LocalIP: "192.168.1.10",
		EgressCountry: "United States", EgressRegion: "California",
	})
	if got != PresenceRoaming {
		t.Fatalf("overseas egress must be roaming, got %q", got)
	}
}

// 公司 NAT：出口在所属公司区域内（省|市全同）+ 本机私网 → 在线
func TestResolvePresenceCompanyNatIsOnline(t *testing.T) {
	now, threshold := presenceFixture()
	// 修复目标场景：公司统一出口 NAT + 本机私网 → 在线
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP: "61.142.9.88", LocalIP: "172.20.36.7/24",
		EgressCountry: "中国", EgressRegion: "广东省|东莞市", HomeRegion: "广东省|东莞市",
	})
	if got != PresenceOnline {
		t.Fatalf("company NAT must be online, got %q", got)
	}
}

// 公司未配置 region（空基准）→ 跳过地理维（宁漏报不误报），只保留网络维
func TestResolvePresenceCompanyWithoutRegionSkipsGeo(t *testing.T) {
	now, threshold := presenceFixture()
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP: "114.114.114.114", LocalIP: "192.168.1.10",
		EgressCountry: "中国", EgressRegion: "江苏省|南京市", HomeRegion: "",
	})
	if got != PresenceOnline {
		t.Fatalf("company without region must skip geo dimension, got %q", got)
	}
}

// 市级降级宽口径：任一侧城市段缺失时按省级比对——公司只配省级基准
// （companies.region 只填「广东省」）或库解析不出城市段都不误报
func TestResolvePresenceProvinceLevelFallback(t *testing.T) {
	now, threshold := presenceFixture()
	// 公司基准只配省段：同省异市出口不判漫游（省级宽口径容错）
	got := ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP: "203.0.113.7", LocalIP: "192.168.1.10",
		EgressCountry: "中国", EgressRegion: "广东省|广州市", HomeRegion: "广东省",
	})
	if got != PresenceOnline {
		t.Fatalf("province-only home region must compare at province level, got %q", got)
	}
	// 出口解析不出城市段（省级宽口径）；异省仍要漫游
	got = ResolvePresence(PresenceInput{
		Now: now, HeartbeatTimeout: threshold,
		LastSeenAt: now.Add(-5 * time.Minute),
		PublicIP: "203.0.113.7", LocalIP: "192.168.1.10",
		EgressCountry: "中国", EgressRegion: "江苏省", HomeRegion: "广东省|东莞市",
	})
	if got != PresenceRoaming {
		t.Fatalf("province-level egress in other province must be roaming, got %q", got)
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

// 市级比对核心（regionMismatch + joinRegion）表驱动锁死口径：
// 异省漫游 / 同省异市漫游 / 同城在线 / 城市段缺失降级省级 / 空基准跳过
func TestRegionMismatch(t *testing.T) {
	cases := []struct {
		egress, home string
		want         bool
	}{
		{"江苏省|南京市", "广东省|东莞市", true},  // 异省
		{"广东省|广州市", "广东省|东莞市", true},  // 同省异市（业务规则：也算漫游）
		{"广东省|东莞市", "广东省|东莞市", false}, // 同城
		{"广东省", "广东省|东莞市", false},       // 出口城市段缺失 → 省级宽口径
		{"广东省|广州市", "广东省", false},       // 基准只配省级 → 省级宽口径
		{"江苏省", "广东省|东莞市", true},        // 城市段缺失但异省仍漫游
		{"", "广东省|东莞市", false},            // 解析不出 → 跳过
		{"广东省|东莞市", "", false},            // 未配置基准 → 跳过
		{"", "", false},                      // 双空
	}
	for _, c := range cases {
		if got := regionMismatch(c.egress, c.home); got != c.want {
			t.Errorf("regionMismatch(%q, %q) = %v, want %v", c.egress, c.home, got, c.want)
		}
	}
	if got := joinRegion("广东省", "东莞市"); got != "广东省|东莞市" {
		t.Errorf("joinRegion with city = %q", got)
	}
	if got := joinRegion("广东省", ""); got != "广东省" {
		t.Errorf("joinRegion without city = %q", got)
	}
	if got := joinRegion("", "东莞市"); got != "" {
		t.Errorf("joinRegion without province = %q", got)
	}
}

// 批量判定 + 按公司基准的直接实证（2026-09-26 市级升级）：
// 同一出口 IP，东莞公司资产 → 漫游、苏州公司资产 → 在线——
// 多公司各归各基准，RegionByCompany 注入取代全局省份白名单
func TestResolveAssetPresenceBatch(t *testing.T) {
	now, threshold := presenceFixture()
	geo := PresenceGeo{
		RegionByCompany: map[int64]string{
			1: "广东省|东莞市", // 东莞总部
			2: "江苏省|苏州市", // 苏州分公司
		},
		RegionOf: func(ip string) (string, string, string) {
			switch ip {
			case "61.142.9.88":
				return "中国", "广东省", "东莞市" // 东莞公司出口
			case "203.0.113.8":
				return "中国", "江苏省", "苏州市" // 苏州公司出口
			case "114.114.114.114":
				return "中国", "江苏省", "南京市" // 异省出口
			case "8.8.8.8":
				return "United States", "California", ""
			}
			return "", "", ""
		},
	}
	assets := []Asset{
		{BaseModel: BaseModel{ID: 1}, Device: &Device{LastSeenAt: now.Add(-5 * time.Minute)}}, // 在线
		{BaseModel: BaseModel{ID: 2}, Device: &Device{LastSeenAt: now.Add(-time.Hour)}},      // 失联
		{BaseModel: BaseModel{ID: 3}}, // 无终端：跳过
		// 苏州公司资产在苏州出口 → 在线（本地即基准）
		{BaseModel: BaseModel{ID: 4}, CompanyID: 2, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "172.20.36.7/24", PublicIP: "203.0.113.8"}},
		// 东莞公司资产跑到苏州出口 → 漫游（异省）
		{BaseModel: BaseModel{ID: 5}, CompanyID: 1, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.10", PublicIP: "114.114.114.114"}},
		// 东莞公司资产在公司出口（同城）→ 在线
		{BaseModel: BaseModel{ID: 6}, CompanyID: 1, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.11", PublicIP: "61.142.9.88"}},
		// 苏州公司资产在东莞公司出口 → 漫游（同一出口、反向断言：
		// 资产 6 在线而资产 7 漫游，证明基准按资产所属公司取）
		{BaseModel: BaseModel{ID: 7}, CompanyID: 2, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.12", PublicIP: "61.142.9.88"}},
		// 海外出口 → 漫游
		{BaseModel: BaseModel{ID: 8}, CompanyID: 1, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.13", PublicIP: "8.8.8.8"}},
		// 公司未配置 region（ID 99 未登记）→ 跳过地理维
		{BaseModel: BaseModel{ID: 9}, CompanyID: 99, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.14", PublicIP: "114.114.114.114"}},
	}
	dispatchByAsset := map[int64]*AssetDispatch{
		2: activeDispatch(now.Add(time.Hour), true), // 外派隔离未超期：预期内离线
	}
	ResolveAssetPresence(assets, dispatchByAsset, now, threshold, geo)

	expect := map[int64]string{
		1: PresenceOnline, 2: PresenceDispatchOffline, 3: "",
		4: PresenceOnline, 5: PresenceRoaming, 6: PresenceOnline,
		7: PresenceRoaming, 8: PresenceRoaming, 9: PresenceOnline,
	}
	for i, a := range assets {
		if a.Presence != expect[a.ID] {
			t.Fatalf("asset %d (idx %d): expected presence %q, got %q", a.ID, i, expect[a.ID], a.Presence)
		}
	}
}

// 异地漫游告警（geo_roaming）：判定依据写回 RoamingReason（transient，
// BuildAlerts 直读禁止重算——口径单源）。四 case：网络维 / 地理维 /
// 海外出口 / 非 roaming 不写；非漫游态必须清空防批量复用串台
func TestResolveAssetPresenceRoamingReason(t *testing.T) {
	now, threshold := presenceFixture()
	geo := PresenceGeo{
		RegionByCompany: map[int64]string{1: "广东省|东莞市"},
		RegionOf: func(ip string) (string, string, string) {
			switch ip {
			case "222.92.0.1":
				return "中国", "江苏省", "苏州市" // 苏州出口
			case "8.8.8.8":
				return "United States", "California", ""
			}
			return "", "", ""
		},
	}
	assets := []Asset{
		// 网络维：本机直接持有公网 IP（4G/拨号/直连）
		{BaseModel: BaseModel{ID: 1}, CompanyID: 1, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "223.104.5.6", PublicIP: "223.104.5.6"}},
		// 地理维：东莞公司资产跑到苏州出口
		{BaseModel: BaseModel{ID: 2}, CompanyID: 1, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.10", PublicIP: "222.92.0.1"}},
		// 海外出口
		{BaseModel: BaseModel{ID: 3}, CompanyID: 1, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.11", PublicIP: "8.8.8.8"}},
		// 在线（公司出口 + 本机私网）：不写依据
		{BaseModel: BaseModel{ID: 4}, CompanyID: 1, Device: &Device{
			LastSeenAt: now.Add(-5 * time.Minute), IPAddress: "192.168.1.12", PublicIP: "61.142.9.88"}},
		// 疑似失联：不写依据
		{BaseModel: BaseModel{ID: 5}, CompanyID: 1, Device: &Device{
			LastSeenAt: now.Add(-time.Hour), IPAddress: "223.104.5.9", PublicIP: "223.104.5.9"}},
	}
	ResolveAssetPresence(assets, nil, now, threshold, geo)

	if assets[0].RoamingReason == "" || !strings.Contains(assets[0].RoamingReason, "本机 IP 223.104.5.6") {
		t.Fatalf("network-dim reason must carry local public ip, got %q", assets[0].RoamingReason)
	}
	if reason := assets[1].RoamingReason; !strings.Contains(reason, "222.92.0.1") ||
		!strings.Contains(reason, "江苏省|苏州市") {
		t.Fatalf("geo-dim reason must carry egress ip and region, got %q", reason)
	}
	if reason := assets[2].RoamingReason; !strings.Contains(reason, "8.8.8.8") ||
		!strings.Contains(reason, "United States") {
		t.Fatalf("overseas reason must carry egress ip and country, got %q", reason)
	}
	for _, i := range []int{3, 4} {
		if assets[i].RoamingReason != "" {
			t.Fatalf("non-roaming asset %d must have empty reason, got %q", i, assets[i].RoamingReason)
		}
	}
}

// 依据清空：同一批资产重判为非漫游时旧依据不得残留（批量复用防御）
func TestResolveAssetPresenceReasonClearedOnReResolve(t *testing.T) {
	now, threshold := presenceFixture()
	assets := []Asset{
		{BaseModel: BaseModel{ID: 1}, CompanyID: 1,
			RoamingReason: "出口 IP 222.92.0.1 解析区域 江苏省|苏州市",
			Device: &Device{LastSeenAt: now.Add(-5 * time.Minute),
				IPAddress: "192.168.1.10", PublicIP: "61.142.9.88"}},
	}
	ResolveAssetPresence(assets, nil, now, threshold, PresenceGeo{})
	if assets[0].Presence != PresenceOnline || assets[0].RoamingReason != "" {
		t.Fatalf("re-resolved online asset must drop stale reason, got presence=%q reason=%q",
			assets[0].Presence, assets[0].RoamingReason)
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
