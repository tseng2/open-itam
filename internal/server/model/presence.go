package model

import (
	"net"
	"strings"
	"time"
)

// Presence 联系状态五态（阶段五 A2 失联语义分层）：纯展示计算属性，不落库。
// 判定依据：外派登记 × Device.LastSeenAt × 心跳阈值 × 网络环境（2026-09-26 漫游判定升级）；
// 阈值经调用方注入，禁止硬编码
const (
	PresenceOnline          = "online"           // 在线：心跳在阈值内且判定在公司网络环境
	PresenceRoaming         = "roaming"          // 漫游中：在线但接入公网（本机公网 IP / 出口地理与所属公司区域不符 / 出口海外）
	PresenceDispatchOffline = "dispatch_offline" // 外派离线(预期内)：外派中+保密隔离+未超期且离线，免告警
	PresenceOverdue         = "overdue"           // 超期未归(高危)：外派中且已过预计归期（催归动作优先）
	PresenceMissing         = "missing"          // 疑似失联：无外派豁免且心跳超阈值（保守报警）
)

// PresenceInput ResolvePresence 的输入契约。
// ActiveDispatch 必须是进行中（Status=DispatchStatusActive）的外派记录，无则传 nil。
// 漫游判定双维（2026-09-26 升级，替代「PublicIP 非空」粗判——公司统一
// 出口 NAT 会让全部内网终端误判漫游；同日再升级市级比对，地理维基准
// 从全局省份改为**资产所属公司的区域**——东莞公司的电脑跑到苏州算漫游，
// 苏州公司的不算，多公司各归各基准）：
//   - 网络维：LocalIP 是公网地址（直连公网/4G/拨号）→ 漫游
//   - 地理维：出口 IP（PublicIP）GeoIP 解析为异省、同省异市或海外 → 漫游；
//     GeoIP 解析不出（空段/库不可用）跳过地理维度（宁漏报不误报）
type PresenceInput struct {
	Now              time.Time
	HeartbeatTimeout time.Duration
	LastSeenAt       time.Time
	PublicIP         string // 出口公网 IP（Agent 采集）
	LocalIP          string // 本机主网卡 IP（Agent 采集；可能带 CIDR 后缀）
	EgressCountry    string // 出口 IP 归属国家（GeoIP 注入；空 = 解析不出）
	EgressRegion     string // 出口 IP「省|市」（GeoIP 注入；城市段缺失只保留省段）
	HomeRegion       string // 资产所属公司区域「省|市」（companies.region 注入；空 = 未配置跳过地理维）
	ActiveDispatch   *AssetDispatch
}

// PresenceGeo 地理解析注入（model 保持纯净不依赖 geoip 库）。
// RegionByCompany：资产所属公司 ID → region 基准「省|市」（调用方对
// 本批资产涉及的公司做**批量 IN 查**注入，禁止逐资产查询防 N+1）；
// 未登记 / 空串 = 该公司资产跳过地理维（宁漏报不误报）。
// RegionOf 返回 (国家, 省份, 城市)，解析不出返回三个空串；nil 时跳过地理维
type PresenceGeo struct {
	RegionByCompany map[int64]string
	RegionOf        func(ip string) (country, province, city string)
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
			LocalIP:          a.Device.IPAddress,
			HomeRegion:       geo.RegionByCompany[a.CompanyID],
			ActiveDispatch:   dispatchByAsset[a.ID],
		}
		if geo.RegionOf != nil && in.PublicIP != "" {
			country, province, city := geo.RegionOf(in.PublicIP)
			in.EgressCountry = country
			in.EgressRegion = joinRegion(province, city)
		}
		a.Presence = ResolvePresence(in)
	}
}

// ResolvePresence 计算单台终端的联系状态，判定顺序即优先级：
//  1. 超期未归(高危)：业务归还已违约，无论是否在线都优先标注（催归优先于存活确认）
//  2. 离线：IsolationOffline 外派豁免 → 预期内；否则疑似失联——外派未标隔离而离线
//     属意外离线，保守报警促使管理员核实或补登隔离标记
//  3. 在线分支细分为在线/漫游中（双维判定见 PresenceInput 注释）：
//     本机公网 IP，或出口地理与所属公司区域不符（异省/同省异市/海外）→
//     漫游中；公司统一出口 NAT 下的内网终端（本机私网 + 出口在所属公司
//     区域内）不再误报漫游。同城家宽与公司内网 GeoIP 无法区分（市级
//     颗粒度极限），按在线处理
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
	return regionMismatch(in.EgressRegion, in.HomeRegion)
}

// joinRegion 组装「省|市」比对串（与 companies.region 同口径）：
// 城市段缺失（库中 '0' / 空）时只保留省段——市级信息不完整时
// 由 regionMismatch 按省级宽口径降级
func joinRegion(province, city string) string {
	if province == "" {
		return ""
	}
	if city == "" {
		return province
	}
	return province + "|" + city
}

// regionMismatch 漫游地理维的市级比对：出口「省|市」与公司基准逐段比对。
// 异省即漫游；同省时两侧都带市级段才比对城市（同省异市 = 漫游——
// 东莞公司的电脑在广州也算漫游）；任一侧市级缺失按省级宽口径放行
// （宁漏报不误报，公司只配省级/库解析不出城市的都落这里）；
// 任一侧为空 = 未配置基准 / 解析不出，整体跳过地理维
func regionMismatch(egress, home string) bool {
	if egress == "" || home == "" {
		return false
	}
	eParts := strings.Split(egress, "|")
	hParts := strings.Split(home, "|")
	if eParts[0] != hParts[0] {
		return true
	}
	if len(eParts) < 2 || len(hParts) < 2 {
		return false
	}
	return eParts[1] != hParts[1]
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
