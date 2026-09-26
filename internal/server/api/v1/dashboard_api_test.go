package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// 总览大盘轻聚合端点：gin 集成测试。
// 覆盖：资产状态计数（台账全量含报废、软删行不计）、changes 未 ack 计数、
// 终端活跃/失联判定（offline_threshold 阈值注入，公式同 ResolvePresence）、
// Agent 版本分布（计数降序 + 百分比 + TopN 截断）、全员可读 + 未登录 401、空库零值

// setupDashboardRouter 独立路由树：离线阈值取 15 分钟（与生产缺省同量级）
func setupDashboardRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/dashboard_api_test.db")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})

	if err := store.NewGormStore(store.DB).PutAgentSettings(context.Background(),
		model.AgentSettings{
			HeartbeatIntervalSec: 600, FullIntervalSec: 3600,
			OfflineThresholdSec: 900, // 测试阈值 15 分钟（stale 夹具 1h 必失联）
		}); err != nil {
		t.Fatalf("seed agent settings: %v", err)
	}

	r := gin.New()
	apiV1 := r.Group("/api/v1")
	protected := apiV1.Group("/")
	protected.Use(middleware.AuthMiddleware())
	RegisterDashboardRoutes(protected)
	return r
}

// seedDashboardFixture 夹具：2 在用 + 1 在库 + 1 维修 + 1 报废 + 1 软删在用；
// 终端 3 台（2 台阈值内活跃、1 台超阈值失联，版本 v0.2.5×2 / v0.2.4×1）；
// change_events 2 条未 ack + 1 条已 ack
func seedDashboardFixture(t *testing.T) {
	t.Helper()
	company := model.Company{Name: "大盘测试公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}

	suffix := time.Now().Format("150405.000000")
	seedAsset := func(tag string, status int) {
		t.Helper()
		a := model.Asset{
			CompanyID: company.ID, CategoryID: 1, CategoryName: "台式整机",
			AssetTag: tag + "-" + suffix, Status: status,
		}
		if err := store.DB.Create(&a).Error; err != nil {
			t.Fatalf("seed asset %s: %v", tag, err)
		}
	}
	seedAsset("AST-DB-USE1", model.AssetStatusInUse)
	seedAsset("AST-DB-USE2", model.AssetStatusInUse)
	seedAsset("AST-DB-STK", model.AssetStatusStock)
	seedAsset("AST-DB-REP", model.AssetStatusRepair)
	seedAsset("AST-DB-SCR", model.AssetStatusScrapped)
	seedAsset("AST-DB-DEL", model.AssetStatusInUse)
	// 软删除一台在用资产：大盘状态计数同台账列表口径，软删行不计
	if err := store.DB.Where("asset_tag = ?", "AST-DB-DEL-"+suffix).
		Delete(&model.Asset{}).Error; err != nil {
		t.Fatalf("soft delete asset: %v", err)
	}

	now := time.Now()
	seedDevice := func(id, version string, lastSeen time.Time) {
		t.Helper()
		d := model.AgentDevice{
			DeviceID: id + "-" + suffix, AgentVersion: version,
			LastSeenAt: lastSeen, RegisteredAt: now,
		}
		if err := store.DB.Create(&d).Error; err != nil {
			t.Fatalf("seed device %s: %v", id, err)
		}
	}
	seedDevice("DEV-DB-A1", "v0.2.5", now)                     // 活跃
	seedDevice("DEV-DB-A2", "v0.2.5", now.Add(-5*time.Minute)) // 活跃（阈值内）
	seedDevice("DEV-DB-M1", "v0.2.4", now.Add(-1*time.Hour))   // 失联（超阈值）

	seedChange := func(deviceID string, acked bool) {
		t.Helper()
		e := model.AgentChangeEvent{
			DeviceID: deviceID, Kind: "hardware_change",
			Severity: "warn", Message: "大盘测试事件", Acked: acked, CreatedAt: now,
		}
		if err := store.DB.Create(&e).Error; err != nil {
			t.Fatalf("seed change event: %v", err)
		}
	}
	seedChange("DEV-DB-A1", false)
	seedChange("DEV-DB-A2", false)
	seedChange("DEV-DB-M1", true)
}

func getDashboardSummary(t *testing.T, r http.Handler, token string) (int, map[string]any) {
	t.Helper()
	rec := doDispatchJSON(t, r, http.MethodGet, "/api/v1/dashboard/summary", token, nil)
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	return rec.Code, resp.Data
}

func numField(t *testing.T, m map[string]any, key string) int {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("field %s missing or not number: %+v", key, m)
	}
	return int(v)
}

func TestDashboardSummaryAggregates(t *testing.T) {
	r := setupDashboardRouter(t)
	seedDashboardFixture(t)
	user := userTokenFor(t, 7, "normal_user", "user") // 普通用户：大盘全员可读

	code, data := getDashboardSummary(t, r, user)
	if code != http.StatusOK {
		t.Fatalf("user role must read dashboard summary, http=%d", code)
	}

	assets, _ := data["assets"].(map[string]any)
	if got := numField(t, assets, "total"); got != 5 {
		t.Fatalf("assets.total = %d, want 5 (软删行不计)", got)
	}
	if got := numField(t, assets, "in_use"); got != 2 {
		t.Fatalf("assets.in_use = %d, want 2", got)
	}
	if got := numField(t, assets, "stock"); got != 1 {
		t.Fatalf("assets.stock = %d, want 1", got)
	}
	if got := numField(t, assets, "repair"); got != 1 {
		t.Fatalf("assets.repair = %d, want 1", got)
	}
	if got := numField(t, assets, "scrapped"); got != 1 {
		t.Fatalf("assets.scrapped = %d, want 1 (台账全量含报废)", got)
	}

	if got := numField(t, data, "changes_pending"); got != 2 {
		t.Fatalf("changes_pending = %d, want 2 (只算未 ack)", got)
	}

	devices, _ := data["devices"].(map[string]any)
	if got := numField(t, devices, "total"); got != 3 {
		t.Fatalf("devices.total = %d, want 3", got)
	}
	if got := numField(t, devices, "active"); got != 2 {
		t.Fatalf("devices.active = %d, want 2 (阈值内)", got)
	}
	if got := numField(t, devices, "missing"); got != 1 {
		t.Fatalf("devices.missing = %d, want 1 (超阈值)", got)
	}

	versions, _ := data["agent_versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("agent_versions len = %d, want 2: %+v", len(versions), versions)
	}
	first, _ := versions[0].(map[string]any)
	if first["version"] != "v0.2.5" || numField(t, first, "count") != 2 || numField(t, first, "percent") != 67 {
		t.Fatalf("agent_versions[0] = %+v, want v0.2.5/2/67", first)
	}
	second, _ := versions[1].(map[string]any)
	if second["version"] != "v0.2.4" || numField(t, second, "count") != 1 || numField(t, second, "percent") != 33 {
		t.Fatalf("agent_versions[1] = %+v, want v0.2.4/1/33", second)
	}
}

func TestDashboardSummaryAuthAndEmpty(t *testing.T) {
	r := setupDashboardRouter(t)

	// 未登录 401
	rec := doDispatchJSON(t, r, http.MethodGet, "/api/v1/dashboard/summary", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: http=%d, want 401", rec.Code)
	}

	// 空库：普通用户全零值、版本分布空数组（空态契约）
	user := userTokenFor(t, 8, "empty_user", "user")
	_, data := getDashboardSummary(t, r, user)
	assets, _ := data["assets"].(map[string]any)
	for _, key := range []string{"total", "in_use", "stock", "repair", "scrapped"} {
		if got := numField(t, assets, key); got != 0 {
			t.Fatalf("empty assets.%s = %d, want 0", key, got)
		}
	}
	if got := numField(t, data, "changes_pending"); got != 0 {
		t.Fatalf("empty changes_pending = %d, want 0", got)
	}
	devices, _ := data["devices"].(map[string]any)
	for _, key := range []string{"total", "active", "missing"} {
		if got := numField(t, devices, key); got != 0 {
			t.Fatalf("empty devices.%s = %d, want 0", key, got)
		}
	}
	if versions, ok := data["agent_versions"].([]any); !ok || len(versions) != 0 {
		t.Fatalf("empty agent_versions must be []: %+v", data["agent_versions"])
	}
}

func TestDashboardSummaryVersionTopN(t *testing.T) {
	r := setupDashboardRouter(t)
	now := time.Now()
	// 6 个互异版本各 1 台：分布截断为 Top 5
	for i := 0; i < 6; i++ {
		d := model.AgentDevice{
			DeviceID: "DEV-DB-V" + string(rune('A'+i)), AgentVersion: "v0.9." + string(rune('0'+i)),
			LastSeenAt: now, RegisteredAt: now,
		}
		if err := store.DB.Create(&d).Error; err != nil {
			t.Fatalf("seed device: %v", err)
		}
	}
	admin := adminToken(t)
	_, data := getDashboardSummary(t, r, admin)
	versions, _ := data["agent_versions"].([]any)
	if len(versions) != 5 {
		t.Fatalf("agent_versions len = %d, want 5 (TopN 截断)", len(versions))
	}
}
