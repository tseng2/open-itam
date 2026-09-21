//go:build !windows

package identity

import "os"

// macOS 无注册表，第二份持久化放在用户目录之外的系统位置
const persistPath = "/var/db/itagent-device-id"

// PersistDeviceID 把指纹写入系统级备份文件
func PersistDeviceID(id string) {
	if id == "" {
		return
	}
	_ = os.WriteFile(persistPath, []byte(id), 0o600)
}

// LoadPersistedDeviceID 从备份文件读回指纹
func LoadPersistedDeviceID() string {
	data, err := os.ReadFile(persistPath)
	if err != nil {
		return ""
	}
	return string(data)
}
