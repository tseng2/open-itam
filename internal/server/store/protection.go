package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"itagent/internal/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetProtectionModule 读取防护模块配置；模块从未配置时返回默认关闭状态而非错误，
// 保证存量服务端升级后无配置也能正常下发
func (s *GormStore) GetProtectionModule(ctx context.Context, key string) (model.ProtectionModule, error) {
	var m model.ProtectionModule
	if err := s.db.WithContext(ctx).Where("module_key = ?", key).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.ProtectionModule{ModuleKey: key}, nil
		}
		return model.ProtectionModule{}, fmt.Errorf("get protection module %s: %w", key, err)
	}
	return m, nil
}

// PutProtectionModule 以 upsert 写入模块开关与 Argon2id 密码哈希（密码不落明文）；
// 仅更新 hash 非空时才覆盖，避免重复设置时误清空已有密码
func (s *GormStore) PutProtectionModule(ctx context.Context, m model.ProtectionModule) error {
	if m.ModuleKey != model.ProtectionModuleQuit && m.ModuleKey != model.ProtectionModuleUninstall {
		return fmt.Errorf("unknown protection module: %s", m.ModuleKey)
	}
	existing, err := s.GetProtectionModule(ctx, m.ModuleKey)
	if err != nil {
		return err
	}
	if m.PasswordHash == "" {
		m.PasswordHash = existing.PasswordHash
	}
	m.UpdatedAt = time.Now().UTC()
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "module_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "password_hash", "updated_at"}),
	}).Create(&m).Error
}

// CreateUninstallCode 生成随机卸载验证码并入库：绑定设备 + 限时过期，等待 Agent 端校验
func (s *GormStore) CreateUninstallCode(ctx context.Context, deviceID string, ttl time.Duration) (model.UninstallCode, error) {
	if deviceID == "" {
		return model.UninstallCode{}, fmt.Errorf("device_id required")
	}
	raw, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return model.UninstallCode{}, fmt.Errorf("generate uninstall code: %w", err)
	}
	uc := model.UninstallCode{
		Code:      fmt.Sprintf("%08d", raw.Int64()),
		DeviceID:  deviceID,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	if err := s.db.WithContext(ctx).Create(&uc).Error; err != nil {
		return model.UninstallCode{}, fmt.Errorf("create uninstall code: %w", err)
	}
	return uc, nil
}

// VerifyUninstallCode 校验卸载验证码：设备绑定、未过期、未使用，通过即标记已用（单次有效）
func (s *GormStore) VerifyUninstallCode(ctx context.Context, deviceID, code string) error {
	if deviceID == "" || code == "" {
		return ErrUnauthorized
	}
	var uc model.UninstallCode
	if err := s.db.WithContext(ctx).
		Where("device_id = ? AND code = ? AND used_at IS NULL", deviceID, code).
		Order("id desc").First(&uc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUnauthorized
		}
		return fmt.Errorf("verify uninstall code: %w", err)
	}
	if time.Now().UTC().After(uc.ExpiresAt) {
		return ErrUnauthorized
	}
	res := s.db.WithContext(ctx).Model(&model.UninstallCode{}).
		Where("id = ?", uc.ID).Update("used_at", time.Now().UTC())
	if res.Error != nil {
		return fmt.Errorf("mark uninstall code used: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrUnauthorized
	}
	return nil
}
