package store

import (
	"context"
	"testing"

	"itagent/internal/server/model"
)

// Agent 采集配置单例契约：无行回落内置默认（首次部署合法状态）、
// Save upsert 全字段生效、round-trip 一致
func TestAgentSettingsSingleton(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// 无行：回落内置默认，不算错
	got, err := s.GetAgentSettings(ctx)
	if err != nil {
		t.Fatalf("get empty agent settings: %v", err)
	}
	def := model.DefaultAgentSettings()
	if got != def {
		t.Fatalf("empty store must return default, got %+v", got)
	}

	// 保存自定义值：upsert 落库
	want := model.AgentSettings{
		HeartbeatIntervalSec: 1800, FullIntervalSec: 14400,
		OfflineThresholdSec: 2400, CompanyProvince: "江苏省",
	}
	if err := s.PutAgentSettings(ctx, want); err != nil {
		t.Fatalf("put agent settings: %v", err)
	}
	got, err = s.GetAgentSettings(ctx)
	if err != nil {
		t.Fatalf("get agent settings: %v", err)
	}
	want.ID = model.AgentSettingsSingletonID
	if got.HeartbeatIntervalSec != 1800 || got.FullIntervalSec != 14400 ||
		got.OfflineThresholdSec != 2400 || got.CompanyProvince != "江苏省" || got.ID != want.ID {
		t.Fatalf("round-trip mismatch: got %+v", got)
	}

	// 二次保存：单例 upsert 覆盖而非新行
	want.HeartbeatIntervalSec = 3600
	if err := s.PutAgentSettings(ctx, want); err != nil {
		t.Fatalf("second put: %v", err)
	}
	got, err = s.GetAgentSettings(ctx)
	if err != nil || got.HeartbeatIntervalSec != 3600 {
		t.Fatalf("second put must upsert singleton: %+v err=%v", got, err)
	}
}
