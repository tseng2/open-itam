package store

import (
	"context"
	"testing"
	"time"

	"itagent/internal/server/model"
)

func TestWebhookAlertConfigDefaultsWhenMissing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// 存量服务端升级后未配置过：返回安全默认而非错误，引擎按未启用处理
	cfg, err := s.GetWebhookAlertConfig(ctx)
	if err != nil {
		t.Fatalf("get missing config: %v", err)
	}
	if cfg.Enabled || cfg.WebhookURL != "" || cfg.Secret != "" {
		t.Fatalf("expected zero config, got %+v", cfg)
	}
	if cfg.CooldownMinutes != model.DefaultWebhookCooldownMinutes {
		t.Fatalf("expected default cooldown %d, got %d", model.DefaultWebhookCooldownMinutes, cfg.CooldownMinutes)
	}
}

func TestWebhookAlertConfigRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	first := model.WebhookAlertConfig{
		Enabled:         true,
		WebhookURL:      "https://hooks.example.com/itam",
		Secret:          "signing-secret",
		CooldownMinutes: 30,
	}
	if err := s.PutWebhookAlertConfig(ctx, first); err != nil {
		t.Fatalf("put config: %v", err)
	}
	got, err := s.GetWebhookAlertConfig(ctx)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if !got.Enabled || got.WebhookURL != first.WebhookURL || got.Secret != first.Secret ||
		got.CooldownMinutes != 30 {
		t.Fatalf("config round-trip mismatch: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("updated_at must be stamped by store")
	}

	// 二次写入是原地更新（单例 ID=1），不是新增一行
	second := model.WebhookAlertConfig{
		Enabled:         false,
		WebhookURL:      "https://hooks.example.com/v2",
		Secret:          "",
		CooldownMinutes: model.DefaultWebhookCooldownMinutes,
	}
	if err := s.PutWebhookAlertConfig(ctx, second); err != nil {
		t.Fatalf("put config again: %v", err)
	}
	got, err = s.GetWebhookAlertConfig(ctx)
	if err != nil {
		t.Fatalf("get config again: %v", err)
	}
	if got.Enabled || got.WebhookURL != second.WebhookURL {
		t.Fatalf("config update mismatch: %+v", got)
	}

	var count int
	// 单例语义：全表只能有一行配置
	if s, ok := s.(*SQLiteStore); ok {
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM webhook_alert_config`).Scan(&count)
	}
	if count != 1 {
		t.Fatalf("webhook_alert_config must stay singleton, got %d rows", count)
	}
}

func TestWebhookAlertStateUpsertByKey(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	t1 := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	t2 := t1.Add(90 * time.Minute)

	// 首次推送 → 落一条状态
	if err := s.PutWebhookAlertState(ctx, model.WebhookAlertState{
		CompanyID: 1, AssetID: 101, AlertType: model.WebhookAlertOverdue, SentAt: t1,
	}); err != nil {
		t.Fatalf("put state: %v", err)
	}
	// 同 (company, asset, type) 再推 → 原地刷新 sent_at，不新增行
	if err := s.PutWebhookAlertState(ctx, model.WebhookAlertState{
		CompanyID: 1, AssetID: 101, AlertType: model.WebhookAlertOverdue, SentAt: t2,
	}); err != nil {
		t.Fatalf("put state again: %v", err)
	}
	// 不同类型/不同资产 → 独立冷却记录
	if err := s.PutWebhookAlertState(ctx, model.WebhookAlertState{
		CompanyID: 1, AssetID: 101, AlertType: model.WebhookAlertMissing, SentAt: t2,
	}); err != nil {
		t.Fatalf("put missing state: %v", err)
	}
	if err := s.PutWebhookAlertState(ctx, model.WebhookAlertState{
		CompanyID: 2, AssetID: 101, AlertType: model.WebhookAlertOverdue, SentAt: t2,
	}); err != nil {
		t.Fatalf("put cross-company state: %v", err)
	}

	states, err := s.ListWebhookAlertStates(ctx)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	if len(states) != 3 {
		t.Fatalf("expected 3 distinct states, got %d: %+v", len(states), states)
	}
	for _, st := range states {
		if st.CompanyID == 1 && st.AssetID == 101 && st.AlertType == model.WebhookAlertOverdue {
			if !st.SentAt.Equal(t2) {
				t.Fatalf("overdue state sent_at must be refreshed to t2, got %v", st.SentAt)
			}
		}
	}
}
