package model

import (
	"time"
)

// 防护模块标识：防退出 / 防卸载，两个独立模块，配置与密码均归服务端集中管理
const (
	ProtectionModuleQuit      = "quit"
	ProtectionModuleUninstall = "uninstall"
)

// UninstallCodeTTL 卸载验证码有效期：QAX 同款限时 10 分钟模型
const UninstallCodeTTL = 10 * time.Minute

// ProtectionModule 全局防护模块配置：各模块独立启用开关 + Argon2id 密码哈希，
// 经 GET /api/v1/agent/config 下发到终端，按注册表双写模式持久化；密码永不落明文
type ProtectionModule struct {
	ModuleKey    string    `gorm:"type:varchar(32);primaryKey" json:"module_key"`
	Enabled      bool      `gorm:"not null;default:false" json:"enabled"`
	PasswordHash string    `gorm:"type:varchar(255);not null;default:''" json:"-"`
	UpdatedAt    time.Time `gorm:"not null" json:"updated_at"`
}

func (ProtectionModule) TableName() string { return "protection_modules" }

// UninstallCode 随机卸载验证码：绑定单台设备、限时过期、单次使用，通过即标记已用
type UninstallCode struct {
	BaseModel
	Code      string     `gorm:"type:varchar(16);uniqueIndex;not null" json:"code"`
	DeviceID  string     `gorm:"type:varchar(64);index;not null" json:"device_id"`
	ExpiresAt time.Time  `gorm:"not null" json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

func (UninstallCode) TableName() string { return "uninstall_codes" }
