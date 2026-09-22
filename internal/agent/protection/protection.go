// Package protection 管理防退出/防卸载两个独立模块的策略：配置与密码归服务端集中管理，
// 经 agent/config 下发后在终端按注册表双写模式持久化；模块开启即要求验证，
// 密码哈希为空时无法通过验证（fail-closed），避免只开开关不设密码留下绕过面
package protection

import (
	"encoding/json"
	"fmt"
)

// ModuleState 单个防护模块的下发状态：启用开关 + Argon2id 密码哈希
type ModuleState struct {
	Enabled      bool   `json:"enabled"`
	PasswordHash string `json:"password_hash"`
}

// Policy 防退出/防卸载两模块策略，与服务端 agent/config 响应字段一一对应
type Policy struct {
	Quit      ModuleState `json:"quit_protection"`
	Uninstall ModuleState `json:"uninstall_protection"`
}

// ParseServerConfig 从 GET /api/v1/agent/config 响应体解析防护策略；
// 字段缺失视为模块关闭（存量服务端兼容），响应体非法返回错误由调用方保持本地既有策略
func ParseServerConfig(body []byte) (Policy, error) {
	var raw struct {
		Code      int          `json:"code"`
		Quit      *ModuleState `json:"quit_protection"`
		Uninstall *ModuleState `json:"uninstall_protection"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Policy{}, fmt.Errorf("parse agent config: %w", err)
	}
	p := Policy{}
	if raw.Quit != nil {
		p.Quit = *raw.Quit
	}
	if raw.Uninstall != nil {
		p.Uninstall = *raw.Uninstall
	}
	return p, nil
}

// serialize 把策略序列化为 JSON 字符串（持久化的公共逻辑，平台壳共用）
func serialize(p Policy) (string, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal protection policy: %w", err)
	}
	return string(data), nil
}

// parsePersisted 解析持久化的 JSON 策略；空值或非法内容视为未持久化
func parsePersisted(data string) (Policy, bool) {
	if data == "" {
		return Policy{}, false
	}
	var p Policy
	if json.Unmarshal([]byte(data), &p) != nil {
		return Policy{}, false
	}
	return p, true
}
