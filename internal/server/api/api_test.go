package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itagent/internal/server/store"
	"itagent/internal/shared/protocol"
)

func setup(t *testing.T) *Handler {
	t.Helper()
	s, err := store.OpenSQLite(t.TempDir() + "/api_test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewHandler(s, Config{
		InstallToken:       "install-secret",
		AdminToken:         "admin-secret",
		DefaultHeartbeatSec: 600,
		DefaultFullSec:      3600,
	})
}

func register(t *testing.T, h http.Handler, deviceID string) string {
	t.Helper()
	body, _ := json.Marshal(protocol.RegisterRequest{
		DeviceID: deviceID, Hostname: "PC-1", OS: "Windows 11",
		AgentVersion: "1.0.0", InstallToken: "install-secret",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/register", bytes.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp protocol.RegisterResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode register resp: %v", err)
	}
	if resp.DeviceToken == "" {
		t.Fatal("register returned empty device token")
	}
	return resp.DeviceToken
}

func TestRegisterRejectsBadInstallToken(t *testing.T) {
	h := setup(t)
	body, _ := json.Marshal(protocol.RegisterRequest{
		DeviceID: "dev-bad", InstallToken: "wrong",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/register", bytes.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRegisterValidation(t *testing.T) {
	h := setup(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/register", strings.NewReader("not-json"))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: expected 400, got %d", rec.Code)
	}

	body, _ := json.Marshal(map[string]string{"install_token": "install-secret"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/register", strings.NewReader(string(body)))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty device_id: expected 400, got %d", rec.Code)
	}
}

func TestAgentConfig(t *testing.T) {
	h := setup(t)
	token := register(t, h, "dev-001")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/config?device_id=dev-001", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent config status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"heartbeat_interval_sec":600`) {
		t.Fatalf("unexpected agent config: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent/config?device_id=dev-001", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestGetDeviceNotFound(t *testing.T) {
	h := setup(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/ghost", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetDevice(t *testing.T) {
	h := setup(t)
	register(t, h, "dev-001")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/dev-001", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "dev-001") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestIngestBadJSON(t *testing.T) {
	h := setup(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader("{corrupt"))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestIngestFlow(t *testing.T) {
	h := setup(t)
	token := register(t, h, "dev-001")

	hb, _ := json.Marshal(protocol.HeartbeatPayload{
		Hostname:  "DG-FIN-0231",
		OS:        protocol.OSInfo{Name: "Windows 11 Pro"},
		Logon:     protocol.LogonInfo{User: "zhangsan", Domain: "DG", Type: protocol.LogonTypeAD, LogonAt: time.Now()},
		UptimeSec: 100,
	})
	env, _ := json.Marshal(protocol.Envelope{
		DeviceID: "dev-001", AgentVersion: "1.0.0",
		ReportType: protocol.ReportTypeHeartbeat, ReportedAt: time.Now(),
		Payload: hb,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(env))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ingest status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp protocol.IngestResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode ingest resp: %v", err)
	}
	if resp.Code != 0 || resp.NextHeartbeatSec != 600 || resp.NextFullSec != 3600 {
		t.Fatalf("unexpected ingest response: %+v", resp)
	}
}

func TestIngestRejectsBadAuth(t *testing.T) {
	h := setup(t)
	token := register(t, h, "dev-001")

	env, _ := json.Marshal(protocol.Envelope{
		DeviceID: "dev-001", ReportType: protocol.ReportTypeHeartbeat, ReportedAt: time.Now(),
		Payload: json.RawMessage(`{}`),
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(env))
	req.Header.Set("Authorization", "Bearer "+token+"x")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestIngestValidatesEnvelope(t *testing.T) {
	h := setup(t)
	token := register(t, h, "dev-001")

	env, _ := json.Marshal(protocol.Envelope{
		DeviceID: "dev-001", ReportType: "garbage", ReportedAt: time.Now(),
		Payload: json.RawMessage(`{}`),
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(env))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestQueryDevicesAndHistory(t *testing.T) {
	h := setup(t)
	token := register(t, h, "dev-001")

	env, _ := json.Marshal(protocol.Envelope{
		DeviceID: "dev-001", AgentVersion: "1.0.0",
		ReportType: protocol.ReportTypeHeartbeat, ReportedAt: time.Now(),
		Payload: json.RawMessage(`{"hostname":"X"}`),
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(env))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("devices status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "dev-001") {
		t.Fatalf("devices list missing dev-001: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), token) {
		t.Fatal("devices list leaks device token")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices/dev-001/history", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"hostname":"X"`) {
		t.Fatalf("history missing payload: %s", rec.Body.String())
	}
}

func TestQueryRequiresAdminToken(t *testing.T) {
	h := setup(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without admin token, got %d", rec.Code)
	}
}
