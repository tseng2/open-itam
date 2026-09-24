package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"itagent/internal/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// webhookAlertConfigID Webhook 告警配置单例固定主键
const webhookAlertConfigID = 1

// GetWebhookAlertConfig 读取告警配置；从未配置时返回安全默认（未启用 + 默认冷却窗口）
// 而非错误，保证存量服务端升级后引擎安静空转
func (s *GormStore) GetWebhookAlertConfig(ctx context.Context) (model.WebhookAlertConfig, error) {
	var cfg model.WebhookAlertConfig
	if err := s.db.WithContext(ctx).First(&cfg, webhookAlertConfigID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.WebhookAlertConfig{CooldownMinutes: model.DefaultWebhookCooldownMinutes}, nil
		}
		return model.WebhookAlertConfig{}, fmt.Errorf("get webhook alert config: %w", err)
	}
	if cfg.CooldownMinutes <= 0 {
		cfg.CooldownMinutes = model.DefaultWebhookCooldownMinutes
	}
	return cfg, nil
}

// PutWebhookAlertConfig 覆写单例配置行（Save 主键非零即 upsert：存在则全字段更新，不存在则插入）
func (s *GormStore) PutWebhookAlertConfig(ctx context.Context, cfg model.WebhookAlertConfig) error {
	cfg.ID = webhookAlertConfigID
	cfg.UpdatedAt = time.Now().UTC()
	if err := s.db.WithContext(ctx).Save(&cfg).Error; err != nil {
		return fmt.Errorf("put webhook alert config: %w", err)
	}
	return nil
}

// ListWebhookAlertStates 全量读取告警冷却状态（每资产至多两条，量级可控）
func (s *GormStore) ListWebhookAlertStates(ctx context.Context) ([]model.WebhookAlertState, error) {
	var states []model.WebhookAlertState
	if err := s.db.WithContext(ctx).Order("id").Find(&states).Error; err != nil {
		return nil, fmt.Errorf("list webhook alert states: %w", err)
	}
	return states, nil
}

// PutWebhookAlertState 按 (company_id, asset_id, alert_type) 唯一键 upsert 推送时间
func (s *GormStore) PutWebhookAlertState(ctx context.Context, st model.WebhookAlertState) error {
	st.UpdatedAt = time.Now().UTC()
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "company_id"}, {Name: "asset_id"}, {Name: "alert_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"sent_at", "updated_at"}),
	}).Create(&st).Error; err != nil {
		return fmt.Errorf("put webhook alert state: %w", err)
	}
	return nil
}
