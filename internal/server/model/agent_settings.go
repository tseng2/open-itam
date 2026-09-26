package model

import (
	"time"
)

// AgentSettings Agent 采集与失联判定的服务端单例配置（ID 恒为 1，
// Web 设置页维护；读写面 admin，Agent 经 agent/config 与 ingest 响应接收下发）。
// 事实唯一源：本表有效行即生效值（设置页保存立即生效，无需重启）；
// 无行 / 查询失败回落内置默认。server.json 的同名配置已降级为
// 「本表不可用时的兜底」，不再作为主配置源。
//
// 联动红线：offline_threshold_sec 必须大于 heartbeat_interval_sec——
// 阈值若小于心跳周期，健康终端的心跳间隔本身就击穿阈值（全员假失联），
// 保存时由 API 层校验拒收
type AgentSettings struct {
	ID                   int64     `gorm:"primaryKey" json:"id"`
	HeartbeatIntervalSec int       `json:"heartbeat_interval_sec"`                   // 心跳上报周期（秒）
	FullIntervalSec      int       `json:"full_interval_sec"`                        // 全量上报周期（秒）
	OfflineThresholdSec  int       `json:"offline_threshold_sec"`                    // 失联判定阈值（秒），须大于心跳周期
	CompanyProvince      string    `gorm:"type:varchar(32)" json:"company_province"` // 公司所在省（GeoIP 出口省份比对的基准，与 ip2region 库名对齐）
	UpdatedAt            time.Time `json:"updated_at"`
}

func (AgentSettings) TableName() string { return "agent_settings" }

// AgentSettingsSingletonID 单例行 ID：整表恒一行，禁止多行
const AgentSettingsSingletonID = 1

// 采集与判定默认值（用户 2026-09-26 拍板：心跳 1 小时 / full 6 小时，
// 阈值 65 分钟略大于心跳；公司所在省缺省广东省——ip2region v4 库名口径）
const (
	DefaultHeartbeatIntervalSec = 3600
	DefaultFullIntervalSec      = 21600
	DefaultOfflineThresholdSec  = 3900
	DefaultCompanyProvince      = "广东省"
)

// DefaultAgentSettings 内置缺省（无行 / 查询失败时的回落值）
func DefaultAgentSettings() AgentSettings {
	return AgentSettings{
		ID:                  AgentSettingsSingletonID,
		HeartbeatIntervalSec: DefaultHeartbeatIntervalSec,
		FullIntervalSec:     DefaultFullIntervalSec,
		OfflineThresholdSec: DefaultOfflineThresholdSec,
		CompanyProvince:     DefaultCompanyProvince,
	}
}

// ResolveOfflineThreshold 失联阈值归一：0/负值回落内置默认（与
// AgentSettings 缺省同源），非正配置不许产生零时长（否则全员秒级失联）。
// 原实现位于老栈 api 包（server.json 注入时代的入口工具），2026-09-26
// 阈值唯一源迁入 agent_settings 后移到 model 供各栈共用，口径只此一处
func ResolveOfflineThreshold(sec int) time.Duration {
	if sec <= 0 {
		return time.Duration(DefaultOfflineThresholdSec) * time.Second
	}
	return time.Duration(sec) * time.Second
}
