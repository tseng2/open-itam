package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// P2 员工自助门户：gin 集成测试。
// 覆盖：个人统计四卡（我的设备数/待审批申请/持有天数/30 天内到期归期）、
// 我的设备边界（不含报废与他人设备）、我的申请边界（传他人 applicant_id
// 也只看自己）、跨用户互不可见

func setupPortalRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/portal_api_test.db")
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
	RegisterPortalRoutes(protected)
	return r
}

func seedPortalFixture(t *testing.T) (companyID, aliceID, bobID, adminID int64) {
	t.Helper()
	company := model.Company{Name: "门户测试公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	alice := model.User{CompanyID: company.ID, Username: "alice", RealName: "张三", Role: "user", Status: "active"}
	bob := model.User{CompanyID: company.ID, Username: "bob", RealName: "李四", Role: "user", Status: "active"}
	admin := model.User{CompanyID: company.ID, Username: "itadmin", RealName: "管理员", Role: "admin", Status: "active"}
	for _, u := range []*model.User{&alice, &bob, &admin} {
		if err := store.DB.Create(u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	suffix := time.Now().Format("150405.000000")
	mustAsset := func(tag string, status int, userID *int64) model.Asset {
		t.Helper()
		a := model.Asset{
			CompanyID: company.ID, CategoryID: 2, CategoryName: "笔记本",
			AssetTag: tag + "-" + suffix, Brand: "DELL", Status: status,
		}
		a.UserID = userID
		if err := store.DB.Create(&a).Error; err != nil {
			t.Fatalf("seed asset %s: %v", tag, err)
		}
		return a
	}
	aliceNotebook := mustAsset("AST-PT-ANB", model.AssetStatusInUse, &alice.ID) // 张三在用
	mustAsset("AST-PT-REP", model.AssetStatusRepair, &alice.ID)               // 张三维修中（仍是我的设备）
	mustAsset("AST-PT-SCRAP", model.AssetStatusScrapped, &alice.ID)            // 已报废：不算我的设备
	mustAsset("AST-PT-BOB", model.AssetStatusInUse, &bob.ID)                   // 他人设备

	// 持有天数：张三最早领用事件在 40 天前
	event := model.AssetEvent{
		AssetID: aliceNotebook.ID, EventType: model.AssetEventAssign,
		Title: "领用审批通过（长期）", TargetPerson: "张三",
	}
	if err := store.DB.Create(&event).Error; err != nil {
		t.Fatalf("seed assign event: %v", err)
	}
	if err := store.DB.Model(&model.AssetEvent{}).Where("id = ?", event.ID).
		UpdateColumn("created_at", time.Now().UTC().AddDate(0, 0, -40)).Error; err != nil {
		t.Fatalf("backdate event: %v", err)
	}

	// 申请：张三 1 待审批 + 1 已批（10 天内归期 → 到期提醒）+ 1 已批（60 天外）
	now := time.Now().UTC()
	seedRequest := func(assetID, applicantID int64, status int, returnAt *time.Time) {
		t.Helper()
		r := model.AssetRequest{
			CompanyID: company.ID, AssetID: assetID, ApplicantID: applicantID,
			ApplicantName: "张三", Status: status, Reason: "门户测试",
			ExpectedReturnAt: returnAt,
		}
		if err := store.DB.Create(&r).Error; err != nil {
			t.Fatalf("seed request: %v", err)
		}
	}
	seedRequest(aliceNotebook.ID, alice.ID, model.AssetRequestStatusPending, nil)
	seedRequest(aliceNotebook.ID, alice.ID, model.AssetRequestStatusApproved, &[]time.Time{now.AddDate(0, 0, 10)}[0])
	seedRequest(aliceNotebook.ID, alice.ID, model.AssetRequestStatusApproved, &[]time.Time{now.AddDate(0, 0, 60)}[0])
	seedRequest(aliceNotebook.ID, bob.ID, model.AssetRequestStatusPending, nil) // 他人申请不计入

	return company.ID, alice.ID, bob.ID, admin.ID
}

func getPortalJSON(t *testing.T, r http.Handler, path, token string) map[string]any {
	t.Helper()
	rec := doDispatchJSON(t, r, http.MethodGet, path, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: http=%d body=%s", path, rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return resp.Data
}

func TestPortalSummary(t *testing.T) {
	r := setupPortalRouter(t)
	companyID, aliceID, bobID, _ := seedPortalFixture(t)
	alice := userTokenFor(t, aliceID, "alice", "user")
	bob := userTokenFor(t, bobID, "bob", "user")
	_ = companyID

	summary := getPortalJSON(t, r, "/api/v1/portal/summary", alice)
	if got := int(summary["device_count"].(float64)); got != 2 {
		t.Fatalf("alice device_count: %+v", summary)
	}
	if got := int(summary["pending_request_count"].(float64)); got != 1 {
		t.Fatalf("alice pending: %+v", summary)
	}
	if got := int(summary["days_in_use"].(float64)); got != 40 {
		t.Fatalf("alice days_in_use: %+v", summary)
	}
	if got := int(summary["expiring_count"].(float64)); got != 1 {
		t.Fatalf("alice expiring: %+v", summary)
	}

	// 他人视角：bob 只有 1 台设备、无申请、无持有履历
	bobSummary := getPortalJSON(t, r, "/api/v1/portal/summary", bob)
	if got := int(bobSummary["device_count"].(float64)); got != 1 {
		t.Fatalf("bob device_count: %+v", bobSummary)
	}
	if got := int(bobSummary["pending_request_count"].(float64)); got != 1 {
		t.Fatalf("bob pending: %+v", bobSummary)
	}
	if got := int(bobSummary["days_in_use"].(float64)); got != 0 {
		t.Fatalf("bob days_in_use: %+v", bobSummary)
	}
	if got := int(bobSummary["expiring_count"].(float64)); got != 0 {
		t.Fatalf("bob expiring: %+v", bobSummary)
	}
}

func TestPortalMyAssetsAndRequests(t *testing.T) {
	r := setupPortalRouter(t)
	companyID, aliceID, bobID, _ := seedPortalFixture(t)
	_ = companyID
	alice := userTokenFor(t, aliceID, "alice", "user")
	bob := userTokenFor(t, bobID, "bob", "user")

	// 我的设备：张三 = 在用 + 维修中，不含报废与他人设备；带维度富化字段
	data := getPortalJSON(t, r, "/api/v1/portal/my-assets", alice)
	items := data["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("alice my-assets: %+v", items)
	}
	for _, raw := range items {
		asset := raw.(map[string]any)
		if asset["brand"] != "DELL" {
			t.Fatalf("enrichment missing: %+v", asset)
		}
	}
	if got := int(data["total"].(float64)); got != 2 {
		t.Fatalf("alice my-assets total: %+v", data)
	}

	// 我的申请：3 条（1 待审批 + 2 已批）；传他人 applicant_id 也只看自己
	data = getPortalJSON(t, r, fmt.Sprintf("/api/v1/portal/my-requests?applicant_id=%d", bobID), alice)
	if got := int(data["total"].(float64)); got != 3 {
		t.Fatalf("alice my-requests must be 3 regardless of applicant_id: %+v", data)
	}

	// bob 视角：1 条申请、1 台设备
	data = getPortalJSON(t, r, "/api/v1/portal/my-requests", bob)
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("bob my-requests: %+v", data)
	}
}
