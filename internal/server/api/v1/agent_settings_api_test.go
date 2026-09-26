package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// Agent 采集配置面契约：GET 未保存回落内置默认、PUT 联动校验
//（阈值必须大于心跳/full 不小于心跳/正整数）、保存后生效读一致、
// user 角色 403（读写均 admin）、未登录 401

func setupAgentSettingsRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := store.InitDB("sqlite", t.TempDir()+"/agent_settings_api_test.db")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	r := gin.New()
	apiV1 := r.Group("/api/v1")
	protected := apiV1.Group("/")
	protected.Use(middleware.AuthMiddleware())
	RegisterAgentSettingsRoutes(protected)
	return r
}

func doSettingsJSON(t *testing.T, r http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doDispatchJSON(t, r, method, path, token, body)
}

func decodeSettingsData(t *testing.T, rec *httptest.ResponseRecorder) model.AgentSettings {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.AgentSettings `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Data
}

func TestAgentSettingsGetReturnsDefaults(t *testing.T) {
	r := setupAgentSettingsRouter(t)
	admin := adminToken(t)

	got := decodeSettingsData(t, doSettingsJSON(t, r, http.MethodGet, "/api/v1/agent-settings", admin, nil))
	def := model.DefaultAgentSettings()
	if got != def {
		t.Fatalf("unsaved store must return defaults, got %+v", got)
	}

	// 未登录 401；user 角色读面也收口 admin
	if rec := doSettingsJSON(t, r, http.MethodGet, "/api/v1/agent-settings", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: http=%d", rec.Code)
	}
	user := userTokenFor(t, 11, "plain_user", "user")
	if rec := doSettingsJSON(t, r, http.MethodGet, "/api/v1/agent-settings", user, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("user role: http=%d", rec.Code)
	}
}

func TestAgentSettingsUpdateValidatesAndPersists(t *testing.T) {
	r := setupAgentSettingsRouter(t)
	admin := adminToken(t)

	// 联动红线：阈值 ≤ 心跳 → 400
	bad := map[string]any{"heartbeat_interval_sec": 3600, "full_interval_sec": 21600, "offline_threshold_sec": 3600, "company_province": "广东省"}
	if rec := doSettingsJSON(t, r, http.MethodPut, "/api/v1/agent-settings", admin, bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("threshold == heartbeat must 400, http=%d", rec.Code)
	}
	// full < heartbeat → 400
	bad["offline_threshold_sec"] = 7200
	bad["full_interval_sec"] = 1800
	if rec := doSettingsJSON(t, r, http.MethodPut, "/api/v1/agent-settings", admin, bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("full < heartbeat must 400, http=%d", rec.Code)
	}
	// 非正数 → 400
	if rec := doSettingsJSON(t, r, http.MethodPut, "/api/v1/agent-settings", admin,
		map[string]any{"heartbeat_interval_sec": 0, "full_interval_sec": 3600, "offline_threshold_sec": 1800}); rec.Code != http.StatusBadRequest {
		t.Fatalf("zero interval must 400, http=%d", rec.Code)
	}

	// 合法保存 → 生效读一致（EffectiveAgentSettings 即此读路径）
	good := map[string]any{"heartbeat_interval_sec": 1800, "full_interval_sec": 10800, "offline_threshold_sec": 2400, "company_province": "江苏省"}
	rec := doSettingsJSON(t, r, http.MethodPut, "/api/v1/agent-settings", admin, good)
	got := decodeSettingsData(t, rec)
	if got.HeartbeatIntervalSec != 1800 || got.FullIntervalSec != 10800 ||
		got.OfflineThresholdSec != 2400 || got.CompanyProvince != "江苏省" {
		t.Fatalf("persisted mismatch: %+v", got)
	}
	if eff := EffectiveAgentSettings(); eff.HeartbeatIntervalSec != 1800 || eff.CompanyProvince != "江苏省" {
		t.Fatalf("effective settings must reflect save immediately: %+v", eff)
	}
	if got := EffectivePresenceTimeout(); got.Seconds() != 2400 {
		t.Fatalf("effective timeout = %v, want 2400s", got)
	}

	// user 角色写面 403
	if rec := doSettingsJSON(t, r, http.MethodPut, "/api/v1/agent-settings",
		userTokenFor(t, 12, "plain_user2", "user"), good); rec.Code != http.StatusForbidden {
		t.Fatalf("user role put: http=%d", rec.Code)
	}
}
