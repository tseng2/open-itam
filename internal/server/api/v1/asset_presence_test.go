package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// setupPresenceRouter 构建带资产路由的测试引擎；离线阈值显式传入，
// 验证"阈值读配置"的注入链路
func setupPresenceRouter(t *testing.T, threshold time.Duration) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/presence_api_test.db")
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
	RegisterAssetRoutes(protected, threshold)
	return r
}

func seedPresenceAsset(t *testing.T, companyID int64, suffix, publicIP string, lastSeen time.Time) int64 {
	t.Helper()
	asset := model.Asset{
		CompanyID:    companyID,
		CategoryID:   2,
		CategoryName: "笔记本",
		AssetTag:     "AST-PRESENCE-" + suffix,
		Status:       20,
	}
	if err := store.DB.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	if lastSeen.IsZero() {
		return asset.ID // 不绑定采集终端
	}
	device := model.Device{
		AssetID:    &asset.ID,
		DeviceID:   "DEV-PRESENCE-" + suffix,
		Hostname:   "PC-" + suffix,
		OSName:     "Windows 11",
		PublicIP:   publicIP,
		LastSeenAt: lastSeen,
	}
	if err := store.DB.Create(&device).Error; err != nil {
		t.Fatalf("seed device: %v", err)
	}
	return asset.ID
}

func seedPresenceDispatch(t *testing.T, companyID, assetID int64, expectedReturn time.Time, isolation bool) {
	t.Helper()
	d := model.AssetDispatch{
		CompanyID:        companyID,
		AssetID:          assetID,
		BorrowerName:     "张三",
		Destination:      "深圳客户现场",
		ExpectedReturnAt: expectedReturn,
		IsolationOffline: isolation,
		Status:           model.DispatchStatusActive,
	}
	if err := store.DB.Create(&d).Error; err != nil {
		t.Fatalf("seed dispatch: %v", err)
	}
}

func fetchPresenceList(t *testing.T, r *gin.Engine, token string) map[int64]string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets?page=1&page_size=50", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items []model.Asset `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("decode list resp: err=%v code=%d body=%s", err, resp.Code, rec.Body.String())
	}
	out := make(map[int64]string, len(resp.Data.Items))
	for _, a := range resp.Data.Items {
		out[a.ID] = a.Presence
	}
	return out
}

func TestAssetsListPresenceLabels(t *testing.T) {
	r := setupPresenceRouter(t, 15*time.Minute)
	company := model.Company{Name: "presence测试公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	companyID := company.ID
	token := adminToken(t)
	now := time.Now().UTC()

	// 一页五态各一：在线 / 漫游中 / 疑似失联 / 超期未归 / 外派离线(预期内)
	online := seedPresenceAsset(t, companyID, "online", "", now.Add(-5*time.Minute))
	roaming := seedPresenceAsset(t, companyID, "roaming", "203.0.113.7", now.Add(-5*time.Minute))
	missing := seedPresenceAsset(t, companyID, "missing", "", now.Add(-time.Hour))
	overdue := seedPresenceAsset(t, companyID, "overdue", "", now.Add(-5*time.Minute))
	seedPresenceDispatch(t, companyID, overdue, now.Add(-24*time.Hour), false)
	expectedOffline := seedPresenceAsset(t, companyID, "iso", "", now.Add(-time.Hour))
	seedPresenceDispatch(t, companyID, expectedOffline, now.Add(24*time.Hour), true)

	presence := fetchPresenceList(t, r, token)
	expect := map[int64]string{
		online:          model.PresenceOnline,
		roaming:         model.PresenceRoaming,
		missing:         model.PresenceMissing,
		overdue:         model.PresenceOverdue,
		expectedOffline: model.PresenceDispatchOffline,
	}
	for assetID, want := range expect {
		if presence[assetID] != want {
			t.Fatalf("asset %d: expected presence %q, got %q", assetID, want, presence[assetID])
		}
	}
}

func TestAssetsListPresenceSkipsAssetsWithoutDevice(t *testing.T) {
	r := setupPresenceRouter(t, 15*time.Minute)
	company := model.Company{Name: "presence无终端公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	token := adminToken(t)

	// 无 Agent 终端（Mac/显示器等）不参与失联判定，presence 留空
	bare := seedPresenceAsset(t, company.ID, "bare", "", time.Time{})
	presence := fetchPresenceList(t, r, token)
	if presence[bare] != "" {
		t.Fatalf("asset without device must have empty presence, got %q", presence[bare])
	}
}

func TestAssetsListPresenceThresholdFromConfig(t *testing.T) {
	// 阈值经 RegisterAssetRoutes 注入（源头是 server.json），
	// 紧阈值下 10 分钟前的心跳已判失联，验证配置链路生效
	r := setupPresenceRouter(t, 5*time.Minute)
	company := model.Company{Name: "presence阈值公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	token := adminToken(t)

	tight := seedPresenceAsset(t, company.ID, "tight", "", time.Now().UTC().Add(-10*time.Minute))
	presence := fetchPresenceList(t, r, token)
	if presence[tight] != model.PresenceMissing {
		t.Fatalf("with 5m threshold, 10m-old heartbeat must be missing, got %q", presence[tight])
	}
}
