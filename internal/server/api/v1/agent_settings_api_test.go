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
// user 角色 403（读写均 admin）、未登录 401。
// 漫游地理基准已迁出本面（companies.region，组织页维护）——
// 响应不再携带 company_province 字段

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

	rec := doSettingsJSON(t, r, http.MethodGet, "/api/v1/agent-settings", admin, nil)
	got := decodeSettingsData(t, rec)
	def := model.DefaultAgentSettings()
	if got != def {
		t.Fatalf("unsaved store must return defaults, got %+v", got)
	}
	// company_province 已删：响应不得再携带该字段
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if data, ok := raw["data"].(map[string]any); ok {
		if _, exists := data["company_province"]; exists {
			t.Fatalf("company_province must be gone from response: %v", data)
		}
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
	bad := map[string]any{"heartbeat_interval_sec": 3600, "full_interval_sec": 21600, "offline_threshold_sec": 3600}
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
	good := map[string]any{"heartbeat_interval_sec": 1800, "full_interval_sec": 10800, "offline_threshold_sec": 2400}
	rec := doSettingsJSON(t, r, http.MethodPut, "/api/v1/agent-settings", admin, good)
	got := decodeSettingsData(t, rec)
	if got.HeartbeatIntervalSec != 1800 || got.FullIntervalSec != 10800 || got.OfflineThresholdSec != 2400 {
		t.Fatalf("persisted mismatch: %+v", got)
	}
	if eff := EffectiveAgentSettings(); eff.HeartbeatIntervalSec != 1800 || eff.OfflineThresholdSec != 2400 {
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

// PresenceGeoFor 契约：按公司 ID 批量取 region 基准（IN 查防 N+1）、
// 空 region / 未登记公司不入映射（跳过地理维）、空 ID 集合安全返回
func TestPresenceGeoForCompanyRegions(t *testing.T) {
	setupAgentSettingsRouter(t) // 只为初始化 store.DB

	dg := model.Company{Name: "区域基准公司甲", Code: "RGA", Region: "广东省|东莞市"}
	if err := store.DB.Create(&dg).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	noRegion := model.Company{Name: "区域基准公司乙", Code: "RGB"}
	if err := store.DB.Create(&noRegion).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	t.Cleanup(func() {
		store.DB.Where("code IN ?", []string{"RGA", "RGB"}).Delete(&model.Company{})
	})

	// 空 ID 集合：无基准、RegionOf 仍注入（网络维保留）
	geo := PresenceGeoFor(nil)
	if geo.RegionByCompany != nil || geo.RegionOf == nil {
		t.Fatalf("empty ids must yield nil region map with resolver: %+v", geo)
	}

	geo = PresenceGeoFor([]int64{dg.ID, noRegion.ID, 999999})
	if len(geo.RegionByCompany) != 1 || geo.RegionByCompany[dg.ID] != "广东省|东莞市" {
		t.Fatalf("region map must only carry non-empty regions: %+v", geo.RegionByCompany)
	}
	if geo.RegionOf == nil {
		t.Fatal("geoip resolver must always be injected")
	}
}
