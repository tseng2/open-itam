//go:build windows

package identity

import (
	"golang.org/x/sys/windows/registry"
)

// 注册表路径：HKLM 需要管理员权限（core-agent 以服务运行，满足）；
// 写失败时降级 HKCU，保证重装 Agent（不动系统）后身份仍可找回
const (
	regPath  = `SOFTWARE\ITAgent`
	regValue = "DeviceID"
)

// PersistDeviceID 把指纹写入注册表，作为配置文件之外的第二份持久化
func PersistDeviceID(id string) {
	if id == "" {
		return
	}
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, regPath, registry.SET_VALUE)
	if err != nil {
		k, _, err = registry.CreateKey(registry.CURRENT_USER, regPath, registry.SET_VALUE)
	}
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetStringValue(regValue, id)
}

// LoadPersistedDeviceID 从注册表读回指纹（配置文件丢失时的找回通道）
func LoadPersistedDeviceID() string {
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		k, err := registry.OpenKey(root, regPath, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue(regValue)
		k.Close()
		if err == nil && v != "" {
			return v
		}
	}
	return ""
}
