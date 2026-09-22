//go:build windows

package protection

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// 复用 identity 的注册表双写模式：HKLM 优先（服务进程满足权限），失败降级 HKCU；
// 与 DeviceID 同路径不同值，卸载时随 SOFTWARE\ITAgent 键树一并清理
const (
	regPath            = `SOFTWARE\ITAgent`
	regValueProtection = "Protection"
)

// Persist 把防护策略写入注册表，作为配置文件之外的第二份持久化
func Persist(p Policy) error {
	data, err := serialize(p)
	if err != nil {
		return err
	}
	if err := persistAt(registry.LOCAL_MACHINE, data); err != nil {
		// HKLM 失败（无管理员权限）降级 HKCU，与 DeviceID 持久化同款降级
		if err := persistAt(registry.CURRENT_USER, data); err != nil {
			return fmt.Errorf("persist protection policy: %w", err)
		}
	}
	return nil
}

// persistAt 把策略写入指定注册表根键下的 Protection 值
func persistAt(root registry.Key, data string) error {
	k, _, err := registry.CreateKey(root, regPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(regValueProtection, data)
}

// Load 从注册表读回防护策略（配置文件丢失或服务端不可达时的本地凭据通道）
func Load() (Policy, bool) {
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		k, err := registry.OpenKey(root, regPath, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue(regValueProtection)
		k.Close()
		if err == nil && v != "" {
			if p, ok := parsePersisted(v); ok {
				return p, true
			}
		}
	}
	return Policy{}, false
}
