package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"itagent/internal/shared/protocol"
)

// 假失联修复回归（2026-09-26）：heartbeat 上报必须同步刷新资产绑定表
// agent_devices.last_seen_at——此前只有 full（1 小时周期）更新它，而
// 联系状态判定用它对比失联阈值，full 周期 > 阈值导致每小时出现
// 「终端健康却判疑似失联」的假失联窗口
func TestHeartbeatTouchesBoundDeviceLastSeen(t *testing.T) {
	h := setupE2E(t)
	token := register(t, h, "dev-touch")
	postFullFor(t, h, token, "dev-touch", "PC-TOUCH", e2eHardware())
	assetID := assetIDOfDevice(t, "dev-touch")

	// full 刚落库：把绑定表 last_seen 回拨 40 分钟（超过任何合理阈值），
	// 模拟「上一轮 full 已超阈值」的假失联窗口起点
	backdate := time.Now().Add(-40 * time.Minute)
	if err := store.DB.Model(&model.Device{}).Where("asset_id = ?", assetID).
		UpdateColumn("last_seen_at", backdate).Error; err != nil {
		t.Fatalf("backdate bound device: %v", err)
	}

	// 发一条纯 heartbeat（非 full）：必须把绑定行 last_seen 拉回当下
	hb, _ := json.Marshal(protocol.HeartbeatPayload{Hostname: "PC-TOUCH"})
	env, _ := json.Marshal(protocol.Envelope{
		DeviceID: "dev-touch", AgentVersion: "0.2.6",
		ReportType: protocol.ReportTypeHeartbeat, ReportedAt: time.Now(),
		Payload: hb,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(env))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("heartbeat ingest: %d %s", rec.Code, rec.Body.String())
	}

	var dev model.Device
	if err := store.DB.Where("asset_id = ?", assetID).First(&dev).Error; err != nil {
		t.Fatalf("reload bound device: %v", err)
	}
	if dev.LastSeenAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("heartbeat must refresh bound device last_seen, still at %v", dev.LastSeenAt)
	}
}
