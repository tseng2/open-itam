package model

import (
	"net"
	"strings"
	"time"
)

// Presence 联系状态五态（阶段五 A2 失联语义分层）：纯展示计算属性，不落库。
// 判定依据：外派登记 × Device.LastSeenAt × 心跳阈值 × 网络环境（2026-09-26 漫游判定升级）；
// 阈值与漫游基准读 agent_settings 配置（经调用方注入），禁止硬编码
const (
	PresenceOnline          = "online"           // 在线：心跳在阈值内且判定在公司网络环境
	PresenceRoaming         = "roaming"          // 漫游中：在线但接入公网（本机公网 IP / 出口异省 / 出口海外）
	PresenceDispatchOffline = "dispatch_offline" // 外派离线(预期内)：外派中+保密隔离+未超期且离线，免告警
	PresenceOverdue         = "overdue"          // 超期未归(高危)：外派中且已过预计归期（催归动作优先）
	PresenceMissing         = "missing"          // 疑似失联：无外派豁免且心跳超阈值（保守报警）
)

// PresenceInput ResolvePresence 的输入契约。
// ActiveDispatch 必须是进行中（Status=DispatchStatusActive）的外派记录，无则传 nil。
// 漫游判定双维（2026-09-26 升级，替代「PublicIP 非空」粗判——公司统一
// 出口 NAT 会让全部内网终端误判漫游）：
//   - 网络维：LocalIP 是公网地址（直连公网/4G/拨号）→ 漫游
//   - 地理维：出口 IP（PublicIP）GeoIP 解析为异省或海外 → 漫游；
//     GeoIP 解析不出（空段/库不可用）跳过地理维度（宁漏报不误报）
type PresenceInput struct {
	Now              time.Time
	HeartbeatTimeout time.Duration
	LastSeenAt       time.Time
	PublicIP         string // 出口公网 IP（Agent 采集）
	LocalIP          string // 本机主网卡 IP（Agent 采集；可能带 CIDR 后缀）
	EgressCountry    string // 出口 IP 归属国家（GeoIP 注入；空 = 解析不出）
	EgressProvince   string // 出口 IP 归属省份（GeoIP 注入；空 = 解析不出）
	HomeProvince     string // 公司所在省（agent_settings 注入；空 = 未配置跳过地理维）
	ActiveDispatch   *AssetDispatch
}

// PresenceGeo 地理解析注入（model 保持纯净不依赖 geoip 库）。
// RegionOf 返回 (国家, 省份)，解析不出返回两个空串；nil 时跳过地理维度
type PresenceGeo struct {
	HomeProvince string
	RegionOf     func(ip string) (country, province string)
}

// ResolveAssetPresence 批量解析资产联系状态并写回 Presence 字段。
// 资产列表富化（A2）与 Webhook 告警扫描（A4）共用的唯一实现，
// 判定核心恒为 ResolvePresence，阈值与地理维度由调用方注入，禁止各自重复实现。
// assets 须已 Preload("Device")；无 Agent 终端跳过（Presence 留空不参与判定）
func ResolveAssetPresence(assets []Asset, dispatchByAsset map[int64]*AssetDispatch, now time.Time, heartbeatTimeout time.Duration, geo PresenceGeo) {
	for i := range assets {
		a := &assets[i]
		if a.Device == nil {
			continue
		}
		in := PresenceInput{
			Now:              now,
			HeartbeatTimeout: heartbeatTimeout,
			LastSeenAt:       a.Device.LastSeenAt,
			PublicIP:         a.Device.PublicIP,
			LocalIP:           a.Device.IPAddress,
			HomeProvince:     geo.HomeProvince,
			ActiveDispatch:   dispatchByAsset[a.ID],
		}
		if geo.RegionOf != nil && in.PublicIP != "" {
			in.EgressCountry, in.EgressProvince = geo.RegionOf(in.PublicIP)
		}
		a.Presence = ResolvePresence(in)
	}
}

// ResolvePresence 计算单台终端的联系状态，判定顺序即优先级：
//  1. 超期未归(高危)：业务归还已违约，无论是否在线都优先标注（催归优先于存活确认）
//  2. 离线：IsolationOffline 外派豁免 → 预期内；否则疑似失联——外派未标隔离而离线
//     属意外离线，保守报警促使管理员核实或补登隔离标记
//  3. 在线分支细分为在线/漫游中（双维判定见 PresenceInput 注释）：
//     本机公网 IP，或出口 IP 异省/海外 → 漫游中；公司统一出口 NAT 下的
//     内网终端（本机私网 + 出口与公司同省）不再误报漫游。
//     同城家宽与公司内网 GeoIP 无法区分（省级颗粒度极限），按在线处理
func ResolvePresence(in PresenceInput) string {
	offline := in.Now.Sub(in.LastSeenAt) > in.HeartbeatTimeout
	if in.ActiveDispatch != nil && in.Now.After(in.ActiveDispatch.ExpectedReturnAt) {
		return PresenceOverdue
	}
	if offline {
		if in.ActiveDispatch != nil && in.ActiveDispatch.IsolationOffline {
			return PresenceDispatchOffline
		}
		return PresenceMissing
	}
	if roamingByNetwork(in) {
		return PresenceRoaming
	}
	return PresenceOnline
}

// roamingByNetwork 漫游双维判定。无出口 IP（纯内网无外网探测能力）不判漫游
func roamingByNetwork(in PresenceInput) bool {
	if in.PublicIP == "" {
		return false
	}
	if isPubliclyRoutedIP(in.LocalIP) {
		return true
	}
	if in.EgressCountry != "" && in.EgressCountry != "中国" {
		return true
	}
	return in.EgressProvince != "" && in.HomeProvince != "" && in.EgressProvince != in.HomeProvince
}

// isPubliclyRoutedIP 本机 IP 是否公网可路由地址：IPv4 排除 RFC1918 私网段，
// IPv6 排除唯一本地地址（fc00::/7）；带 CIDR 后缀（如 172.20.36.7/24）宽容截取。
// 解析失败按非公网处理（宁漏报不误报——解析不了的地址不该触发漫游）
func isPubliclyRoutedIP(raw string) bool {
	if i := strings.IndexByte(raw, '/'); i >= 0 {
		raw = raw[:i]
	}
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || !ip.IsGlobalUnicast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
			if _, private, _ := net.ParseCIDR(cidr); private.Contains(v4) {
				return false
			}
		}
		return true
	}
	// IPv6：唯一本地地址（fc00::/7）等同私网
	if ip[0]&0xfe == 0xfc {
		return false
	}
	return true
}
