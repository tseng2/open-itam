package model

import "time"

// Presence 联系状态五态（阶段五 A2 失联语义分层）：纯展示计算属性，不落库。
// 判定依据：外派登记 × Device.LastSeenAt × 心跳阈值；阈值读配置，禁止硬编码
const (
	PresenceOnline          = "online"           // 在线：心跳在阈值内
	PresenceRoaming         = "roaming"          // 漫游中：在线且走公网（PublicIP 非空粗判，后续接 VPN 网卡识别增强）
	PresenceDispatchOffline = "dispatch_offline" // 外派离线(预期内)：外派中+保密隔离+未超期且离线，免告警
	PresenceOverdue         = "overdue"          // 超期未归(高危)：外派中且已过预计归期（催归动作优先）
	PresenceMissing         = "missing"          // 疑似失联：无外派豁免且心跳超阈值（保守报警）
)

// PresenceInput ResolvePresence 的输入契约。
// ActiveDispatch 必须是进行中（Status=DispatchStatusActive）的外派记录，无则传 nil
type PresenceInput struct {
	Now              time.Time
	HeartbeatTimeout time.Duration
	LastSeenAt       time.Time
	PublicIP         string
	ActiveDispatch   *AssetDispatch
}

// ResolvePresence 计算单台终端的联系状态，判定顺序即优先级：
//  1. 超期未归(高危)：业务归还已违约，无论是否在线都优先标注（催归优先于存活确认）
//  2. 离线：IsolationOffline 外派豁免 → 预期内；否则疑似失联——外派未标隔离而离线
//     属意外离线，保守报警促使管理员核实或补登隔离标记
//  3. 在线：PublicIP 非空粗判为漫游中；隔离标记意味着"应离线"，设备实际在线时
//     如实展示在线/漫游，违规联网的告警语义交给 A4/网卡识别，不在五态内混判
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
	if in.PublicIP != "" {
		return PresenceRoaming
	}
	return PresenceOnline
}
