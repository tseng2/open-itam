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

// P2 消息中心起步（站内信）：gin 集成测试。
// 覆盖：设备申请事件源接线（提交→管理员待办、通过/驳回→申请人回执）、
// 收件箱越权收口（只能读写自己的通知）、已读幂等、未读计数、全部已读

func setupNotificationRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/notification_api_test.db")
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
	RegisterNotificationRoutes(protected)
	// 事件源接线走真实设备申请路由面
	RegisterAssetRequestRoutes(protected)
	return r
}

// seedNotificationFixture 公司 + 申请人（user）+ 竞争人（user）+ 管理员（admin）+ 一台库存资产
func seedNotificationFixture(t *testing.T) (companyID, applicantID, competitorID, adminID, assetID int64) {
	t.Helper()
	company := model.Company{Name: "站内信测试公司-" + t.Name() + time.Now().Format("150405.000000000")}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	applicant := model.User{CompanyID: company.ID, Username: "zhangsan", RealName: "张三", Role: "user", Status: "active"}
	competitor := model.User{CompanyID: company.ID, Username: "lisi", RealName: "李四", Role: "user", Status: "active"}
	admin := model.User{CompanyID: company.ID, Username: "itadmin", RealName: "IT管理员", Role: "admin", Status: "active"}
	for _, u := range []*model.User{&applicant, &competitor, &admin} {
		if err := store.DB.Create(u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	asset := model.Asset{
		CompanyID: company.ID, CategoryID: 2, CategoryName: "笔记本",
		AssetTag: "AST-NTF-" + time.Now().Format("150405.000000000"),
		Status:   model.AssetStatusStock,
	}
	if err := store.DB.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	return company.ID, applicant.ID, competitor.ID, admin.ID, asset.ID
}

func notificationUnreadCount(t *testing.T, r http.Handler, token, query string) int64 {
	t.Helper()
	rec := doDepJSON(t, r, "GET", "/api/v1/notifications/unread-count"+query, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("unread-count: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Count int64 `json:"count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode unread-count: %v", err)
	}
	return resp.Data.Count
}

func listMyNotifications(t *testing.T, r http.Handler, token, query string) []model.Notification {
	t.Helper()
	rec := doDepJSON(t, r, "GET", "/api/v1/notifications"+query, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list notifications: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Items []model.Notification `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode notifications: %v", err)
	}
	return resp.Data.Items
}

func submitRequest(t *testing.T, r http.Handler, token string, companyID, assetID int64) int64 {
	t.Helper()
	// 短期借用必须带归期（store 校验）；长期领用则归期强制清空
	rec := doDepJSON(t, r, "POST", "/api/v1/asset-requests", token, map[string]any{
		"company_id": companyID, "asset_id": assetID, "reason": "出差办公",
		"expected_return_at": time.Now().AddDate(0, 0, 7).UTC().Format(time.RFC3339),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("submit request: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.AssetRequest `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Data.ID
}

func TestNotificationBellSelfService(t *testing.T) {
	r := setupNotificationRouter(t)
	companyID, applicantID, competitorID, adminID, assetID := seedNotificationFixture(t)
	applicant := userTokenFor(t, applicantID, "zhangsan", "user")
	competitor := userTokenFor(t, competitorID, "lisi", "user")
	admin := userTokenFor(t, adminID, "itadmin", "admin")

	// 两个申请人先后提交（竞争同资产放行）→ 公司管理员收到两条待办
	requestID := submitRequest(t, r, applicant, companyID, assetID)
	submitRequest(t, r, competitor, companyID, assetID)
	if got := notificationUnreadCount(t, r, admin, ""); got != 2 {
		t.Fatalf("admin must get 2 pending notifications, got %d", got)
	}
	// 申请人无任何通知（回执尚未发生）
	if got := notificationUnreadCount(t, r, applicant, ""); got != 0 {
		t.Fatalf("applicant unread must be 0, got %d", got)
	}

	// 通知内容与路由跳转锚点；按 ResourceID 定位（不依赖列表顺序）
	items := listMyNotifications(t, r, admin, "?unread=true")
	if len(items) != 2 || items[0].Type != model.NotificationTypeAssetRequest {
		t.Fatalf("admin notifications: %+v", items)
	}
	var mine *model.Notification
	for i := range items {
		if items[i].ResourceID == fmt.Sprintf("%d", requestID) {
			mine = &items[i]
		}
	}
	if mine == nil {
		t.Fatalf("notification for request %d missing: %+v", requestID, items)
	}
	if mine.Title != "新设备申请待审批" || mine.Resource != "asset-requests" {
		t.Fatalf("notification content: %+v", mine)
	}

	// 越权收口：申请人读不到管理员的通知（列表空、标记 404）
	if got := listMyNotifications(t, r, applicant, ""); len(got) != 0 {
		t.Fatalf("applicant must not see admin notifications, got %+v", got)
	}
	rec := doDepJSON(t, r, "POST", fmt.Sprintf("/api/v1/notifications/%d/read", mine.ID), applicant, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user mark must 404, got %d", rec.Code)
	}

	// 管理员标记单条已读：计数 2 → 1；重复标记幂等 200
	rec = doDepJSON(t, r, "POST", fmt.Sprintf("/api/v1/notifications/%d/read", mine.ID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("mark read: http=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := notificationUnreadCount(t, r, admin, ""); got != 1 {
		t.Fatalf("unread after mark: %d", got)
	}
	rec = doDepJSON(t, r, "POST", fmt.Sprintf("/api/v1/notifications/%d/read", mine.ID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-mark must be idempotent 200, got %d", rec.Code)
	}
	// 非法 id → 400；不存在 id → 404
	rec = doDepJSON(t, r, "POST", "/api/v1/notifications/abc/read", admin, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus id must 400, got %d", rec.Code)
	}
	rec = doDepJSON(t, r, "POST", "/api/v1/notifications/999999/read", admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing id must 404, got %d", rec.Code)
	}

	// 全部已读：返回受影响数 1（竞争人那条仍 unread）；幂等空转返回 0
	rec = doDepJSON(t, r, "POST", "/api/v1/notifications/read-all", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read-all: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Affected int64 `json:"affected"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Affected != 1 {
		t.Fatalf("read-all affected: %+v", resp.Data)
	}
	rec = doDepJSON(t, r, "POST", "/api/v1/notifications/read-all", admin, nil)
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Affected != 0 {
		t.Fatalf("read-all idempotent: %+v", resp.Data)
	}
	if got := notificationUnreadCount(t, r, admin, ""); got != 0 {
		t.Fatalf("unread after read-all: %d", got)
	}
}

func TestNotificationDecisionTrail(t *testing.T) {
	r := setupNotificationRouter(t)
	companyID, applicantID, competitorID, adminID, assetID := seedNotificationFixture(t)
	_ = competitorID
	applicant := userTokenFor(t, applicantID, "zhangsan", "user")
	admin := userTokenFor(t, adminID, "itadmin", "admin")

	// 通过 → 申请人收到回执
	id1 := submitRequest(t, r, applicant, companyID, assetID)
	rec := doDepJSON(t, r, "POST", fmt.Sprintf("/api/v1/asset-requests/%d/approve", id1), admin, map[string]any{
		"company_id": companyID, "decision_remark": "同意，尽快领取",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: http=%d body=%s", rec.Code, rec.Body.String())
	}
	items := listMyNotifications(t, r, applicant, "?unread=true")
	if len(items) != 1 || items[0].Title != "设备申请已通过" {
		t.Fatalf("approve receipt: %+v", items)
	}
	if items[0].UserID != applicantID || items[0].CompanyID != companyID {
		t.Fatalf("receipt routing: %+v", items[0])
	}

	// 驳回 → 申请人收到驳回回执（含批注）
	asset2 := model.Asset{
		CompanyID: companyID, CategoryID: 2, CategoryName: "笔记本",
		AssetTag: "AST-NTF2-" + time.Now().Format("150405.000000000"),
		Status:   model.AssetStatusStock,
	}
	if err := store.DB.Create(&asset2).Error; err != nil {
		t.Fatalf("seed asset2: %v", err)
	}
	id2 := submitRequest(t, r, applicant, companyID, asset2.ID)
	rec = doDepJSON(t, r, "POST", fmt.Sprintf("/api/v1/asset-requests/%d/reject", id2), admin, map[string]any{
		"company_id": companyID, "decision_remark": "该设备另有安排",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("reject: http=%d body=%s", rec.Code, rec.Body.String())
	}
	items = listMyNotifications(t, r, applicant, "?unread=true")
	if len(items) != 2 || items[0].Title != "设备申请已驳回" {
		t.Fatalf("reject receipt: %+v", items)
	}
	if items[0].Content == "" {
		t.Fatalf("reject receipt content empty: %+v", items[0])
	}

	// 未读计数 = 2（通过 + 驳回）
	if got := notificationUnreadCount(t, r, applicant, fmt.Sprintf("?company_id=%d", companyID)); got != 2 {
		t.Fatalf("applicant unread: %d", got)
	}
}
