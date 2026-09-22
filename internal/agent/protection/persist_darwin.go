//go:build darwin

package protection

import (
	"encoding/json"
	"fmt"
	"os"
)

// macOS 无注册表：与 identity 一致，写系统级备份文件（0o600 仅属主可读）
const protectionFile = "/var/db/itagent-protection.json"

// Persist 把防护策略写入系统级备份文件，作为配置文件之外的第二份持久化
func Persist(p Policy) error {
	data, err := serialize(p)
	if err != nil {
		return err
	}
	if err := os.WriteFile(protectionFile, []byte(data), 0o600); err != nil {
		return fmt.Errorf("persist protection policy: %w", err)
	}
	return nil
}

// Load 从备份文件读回防护策略（配置文件丢失或服务端不可达时的本地凭据通道）
func Load() (Policy, bool) {
	data, err := os.ReadFile(protectionFile)
	if err != nil {
		return Policy{}, false
	}
	return parsePersisted(string(data))
}
