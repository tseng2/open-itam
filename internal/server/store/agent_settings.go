package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"itagent/internal/server/model"
)

// Agent 采集与失联判定单例配置（agent_settings，ID 恒 1）：
// webhook 单例先例——读面无行回落内置默认，写面 Save 主键 upsert。
// 无行不算错误（首次部署 / 未在设置页保存过都是合法状态）

func (s *GormStore) GetAgentSettings(ctx context.Context) (model.AgentSettings, error) {
	var cfg model.AgentSettings
	if err := s.db.WithContext(ctx).First(&cfg, model.AgentSettingsSingletonID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.DefaultAgentSettings(), nil
		}
		return model.AgentSettings{}, fmt.Errorf("get agent settings: %w", err)
	}
	return cfg, nil
}

func (s *GormStore) PutAgentSettings(ctx context.Context, cfg model.AgentSettings) error {
	cfg.ID = model.AgentSettingsSingletonID
	cfg.UpdatedAt = time.Now().UTC()
	if err := s.db.WithContext(ctx).Save(&cfg).Error; err != nil {
		return fmt.Errorf("put agent settings: %w", err)
	}
	return nil
}
