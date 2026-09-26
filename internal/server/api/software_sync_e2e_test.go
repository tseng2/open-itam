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

// 阶段三数据地基端到端：full 上报 → 终端软件清单结构化落库。
// 全量覆盖语义（卸载即消失）+ 公司归属取自绑定资产 + 空名条目不入库

func postFullWithSoftware(t *testing.T, h http.Handler, token, deviceID, hostname string, software []protocol.Software) {
	t.Helper()
	full := protocol.FullPayload{
		HeartbeatPayload: protocol.HeartbeatPayload{Hostname: hostname, OS: protocol.OSInfo{Name: "Windows 11"}},
		Hardware:         e2eHardware(),
		Software:         software,
	}
	full.Hardware.Serial = "SN-E2E-SW"
	p, _ := json.Marshal(full)
	env, _ := json.Marshal(protocol.Envelope{
		DeviceID: deviceID, ReportType: protocol.ReportTypeFull,
		ReportedAt: time.Now(), Payload: p,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(env))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("full ingest failed: %d %s", rec.Code, rec.Body.String())
	}
}

func deviceSoftwareRows(t *testing.T, deviceID string) []model.DeviceSoftware {
	t.Helper()
	var rows []model.DeviceSoftware
	store.DB.Where("device_id = ?", deviceID).Find(&rows)
	return rows
}

func TestIngestSyncsDeviceSoftware(t *testing.T) {
	h := setupE2E(t)
	token := register(t, h, "dev-sw")

	postFullWithSoftware(t, h, token, "dev-sw", "PC-SW", []protocol.Software{
		{Name: "Microsoft 365 Apps for enterprise", Version: "16.0.1", InstallPath: "C:\\Program Files\\Office"},
		{Name: "AutoCAD 2026", Version: "2026", InstallPath: ""},
		{Name: "", Version: "x", InstallPath: ""}, // 空名残留键不入库
	})
	assetID := assetIDOfDevice(t, "dev-sw")
	var asset model.Asset
	if err := store.DB.First(&asset, assetID).Error; err != nil {
		t.Fatalf("load asset: %v", err)
	}

	rows := deviceSoftwareRows(t, "dev-sw")
	if len(rows) != 2 {
		t.Fatalf("expected 2 software rows (empty name skipped), got %d", len(rows))
	}
	for _, r := range rows {
		if r.CompanyID != asset.CompanyID || r.AssetID != assetID {
			t.Fatalf("company/asset attribution wrong: %+v", r)
		}
		if r.SeenAt.IsZero() {
			t.Fatalf("seen_at must be set: %+v", r)
		}
	}

	// 全量覆盖：第二次上报少一件、多一件 → 行集与最新上报一致（卸载即消失）
	postFullWithSoftware(t, h, token, "dev-sw", "PC-SW", []protocol.Software{
		{Name: "Microsoft 365 Apps for enterprise", Version: "16.0.1", InstallPath: "C:\\Program Files\\Office"},
		{Name: "WPS Office 2023", Version: "2023", InstallPath: ""},
	})
	rows = deviceSoftwareRows(t, "dev-sw")
	if len(rows) != 2 {
		t.Fatalf("full-replace expected 2 rows, got %d", len(rows))
	}
	names := map[string]bool{}
	for _, r := range rows {
		names[r.Name] = true
	}
	if names["AutoCAD 2026"] || !names["WPS Office 2023"] || !names["Microsoft 365 Apps for enterprise"] {
		t.Fatalf("stale software survived replace: %+v", names)
	}

	// 空清单上报：清空该终端全部软件行
	postFullWithSoftware(t, h, token, "dev-sw", "PC-SW", nil)
	if rows := deviceSoftwareRows(t, "dev-sw"); len(rows) != 0 {
		t.Fatalf("empty report must clear rows, got %d", len(rows))
	}
}
