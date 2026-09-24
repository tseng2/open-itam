package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// ---- 测试脚手架（沿用 dispatch_api_test 范式）----

type assetRequestAPIResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func setupAssetRequestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/asset_request_api_test.db")
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
	RegisterAssetRequestRoutes(protected)
	return r
}

// seedAssetRequestFixture 建公司 + 三个用户（普通甲/竞争乙/管理员）+ 三类资产：
// 可申请（库存中未绑定）、已被领用、报废
func seedAssetRequestFixture(t *testing.T) (companyID int64, userID, competitorID, adminID int64, freeAssetID, boundAssetID, scrappedAssetID int64) {
	t.Helper()
	company := model.Company{Name: "申请测试公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	user := model.User{CompanyID: company.ID, Username: "zhangsan", RealName: "张三", Role: "user"}
	competitor := model.User{CompanyID: company.ID, Username: "lisi", RealName: "李四", Role: "user"}
	admin := model.User{CompanyID: company.ID, Username: "itadmin", RealName: "IT管理员", Role: "admin"}
	for _, u := range []*model.User{&user, &competitor, &admin} {
		if err := store.DB.Create(u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	suffix := time.Now().Format("150405.000000")
	mustAsset := func(tag string, status int, userID *int64) model.Asset {
		t.Helper()
		asset := model.Asset{
			CompanyID:    company.ID,
			CategoryID:   2,
			CategoryName: "笔记本",
			AssetTag:     tag + "-" + suffix,
			Brand:        "DELL",
			ModelName:    "Latitude 5450",
			Status:       status,
		}
		asset.UserID = userID
		if err := store.DB.Create(&asset).Error; err != nil {
			t.Fatalf("seed asset: %v", err)
		}
		return asset
	}
	free := mustAsset("AST-REQ-FREE", model.AssetStatusStock, nil)
	bound := mustAsset("AST-REQ-BOUND", model.AssetStatusInUse, &user.ID)
	scrapped := mustAsset("AST-REQ-SCRAP", model.AssetStatusScrapped, nil)
	return company.ID, user.ID, competitor.ID, admin.ID, free.ID, bound.ID, scrapped.ID
}

func userTokenFor(t *testing.T, id int64, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(id, username, role)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

func doAssetRequestJSON(t *testing.T, r http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	return doDispatchJSON(t, r, method, path, token, body)
}

func createRequestViaAPI(t *testing.T, r *gin.Engine, token string, companyID, assetID, applicantID int64, longTerm bool, reason string) model.AssetRequest {
	t.Helper()
	body := gin.H{
		"company_id":  companyID,
		"asset_id":    assetID,
		"is_long_term": longTerm,
		"reason":      reason,
	}
	if applicantID > 0 {
		body["applicant_id"] = applicantID
	}
	if !longTerm {
		body["expected_return_at"] = time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339)
	}
	rec := doAssetRequestJSON(t, r, http.MethodPost, "/api/v1/asset-requests", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create request status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp assetRequestAPIResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	var created model.AssetRequest
	json.Unmarshal(resp.Data, &created)
	return created
}

func loadAsset(t *testing.T, id int64) model.Asset {
	t.Helper()
	var asset model.Asset
	if err := store.DB.First(&asset, id).Error; err != nil {
		t.Fatalf("load asset: %v", err)
	}
	return asset
}

// ---- 用例 ----

func TestAssetRequestCreateApproveBindsAssetInOneTx(t *testing.T) {
	r := setupAssetRequestRouter(t)
	companyID, userID, _, adminID, freeAssetID, _, _ := seedAssetRequestFixture(t)
	userToken := userTokenFor(t, userID, "zhangsan", "user")
	adminToken := userTokenFor(t, adminID, "itadmin", "admin")

	// 普通用户提交申请（本人名义）
	created := createRequestViaAPI(t, r, userToken, companyID, freeAssetID, 0, true, "新项目长期开发用机")
	if created.Status != model.AssetRequestStatusPending || created.ApplicantName != "张三" {
		t.Fatalf("unexpected created: %+v", created)
	}

	// admin 审批通过：单事务内完成 流转 + 绑定 + 台账 10→20 + assign 履历
	rec := doAssetRequestJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/asset-requests/%d/approve", created.ID), adminToken, gin.H{
			"company_id":      companyID,
			"decision_remark": "同意，注意保管",
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", rec.Code, rec.Body.String())
	}
	asset := loadAsset(t, freeAssetID)
	if asset.UserID == nil || *asset.UserID != userID || asset.Status != model.AssetStatusInUse {
		t.Fatalf("asset must be bound to applicant and in-use: %+v", asset)
	}

	// assign 履历：TargetPerson=申请人，短期借用带归期（本例长期无归期）
	var event model.AssetEvent
	if err := store.DB.Where("asset_id = ? AND event_type = ?", freeAssetID, model.AssetEventAssign).First(&event).Error; err != nil {
		t.Fatalf("assign event not recorded: %v", err)
	}
	if event.TargetPerson != "张三" || event.OperatorID == nil || *event.OperatorID != adminID {
		t.Fatalf("assign event incomplete: %+v", event)
	}

	// 重复审批 → 状态不允许
	rec = doAssetRequestJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/asset-requests/%d/approve", created.ID), adminToken, gin.H{"company_id": companyID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("double approve status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssetRequestApproveConflictKeepsPending(t *testing.T) {
	r := setupAssetRequestRouter(t)
	companyID, userID, competitorID, _, freeAssetID, _, _ := seedAssetRequestFixture(t)
	adminToken := userTokenFor(t, 9, "admin", "admin")

	// 两个申请人竞争同一资产：第一个通过后，第二个审批必须 409 且申请保持 pending
	first := createRequestViaAPI(t, r, adminToken, companyID, freeAssetID, userID, true, "竞争申请一")
	second := createRequestViaAPI(t, r, adminToken, companyID, freeAssetID, competitorID, true, "竞争申请二")

	rec := doAssetRequestJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/asset-requests/%d/approve", first.ID), adminToken, gin.H{"company_id": companyID})
	if rec.Code != http.StatusOK {
		t.Fatalf("first approve status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doAssetRequestJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/asset-requests/%d/approve", second.ID), adminToken, gin.H{"company_id": companyID})
	if rec.Code != http.StatusConflict {
		t.Fatalf("second approve must be 409, got %d body=%s", rec.Code, rec.Body.String())
	}
	var reloaded model.AssetRequest
	store.DB.First(&reloaded, second.ID)
	if reloaded.Status != model.AssetRequestStatusPending {
		t.Fatalf("losing request must stay pending (tx rollback), got %+v", reloaded)
	}
	// 竞争失败方不能产生 assign 履历
	var n int64
	store.DB.Model(&model.AssetEvent{}).
		Where("asset_id = ? AND event_type = ?", freeAssetID, model.AssetEventAssign).Count(&n)
	if n != 1 {
		t.Fatalf("expected exactly 1 assign event, got %d", n)
	}
}

func TestAssetRequestCreateGuards(t *testing.T) {
	r := setupAssetRequestRouter(t)
	companyID, userID, _, _, _, boundAssetID, scrappedAssetID := seedAssetRequestFixture(t)
	userToken := userTokenFor(t, userID, "zhangsan", "user")

	// 已被领用的资产 → 400
	rec := doAssetRequestJSON(t, r, http.MethodPost, "/api/v1/asset-requests", userToken, gin.H{
		"company_id": companyID, "asset_id": boundAssetID,
		"is_long_term": true, "reason": "要一台",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bound asset status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 报废资产 → 400
	rec = doAssetRequestJSON(t, r, http.MethodPost, "/api/v1/asset-requests", userToken, gin.H{
		"company_id": companyID, "asset_id": scrappedAssetID,
		"is_long_term": true, "reason": "要一台",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("scrapped asset status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 他司资产 → 404（申请人属本公司，资产不在该公司名下）
	foreignAsset := model.Asset{CompanyID: companyID + 100, AssetTag: "AST-REQ-FOREIGN", Status: model.AssetStatusStock}
	if err := store.DB.Create(&foreignAsset).Error; err != nil {
		t.Fatalf("seed foreign asset: %v", err)
	}
	rec = doAssetRequestJSON(t, r, http.MethodPost, "/api/v1/asset-requests", userToken, gin.H{
		"company_id": companyID, "asset_id": foreignAsset.ID,
		"is_long_term": true, "reason": "要一台",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company asset status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 申请人不属于目标公司（user 越权替他司代录）→ 400
	rec = doAssetRequestJSON(t, r, http.MethodPost, "/api/v1/asset-requests", userToken, gin.H{
		"company_id": companyID + 100, "asset_id": foreignAsset.ID,
		"is_long_term": true, "reason": "要一台",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cross-company applicant status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 缺事由 → 400
	rec = doAssetRequestJSON(t, r, http.MethodPost, "/api/v1/asset-requests", userToken, gin.H{
		"company_id": companyID, "asset_id": scrappedAssetID, "is_long_term": true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing reason status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssetRequestUserScopingAndCancel(t *testing.T) {
	r := setupAssetRequestRouter(t)
	companyID, userID, competitorID, _, freeAssetID, _, _ := seedAssetRequestFixture(t)
	userToken := userTokenFor(t, userID, "zhangsan", "user")
	otherToken := userTokenFor(t, competitorID, "lisi", "user")

	mine := createRequestViaAPI(t, r, userToken, companyID, freeAssetID, 0, true, "我要")
	// 另一用户重复申请同一资产也允许（竞争留待审批裁决）
	createRequestViaAPI(t, r, otherToken, companyID, freeAssetID, 0, true, "他也要")

	// user 视角列表：只看自己的，无论是否传 applicant_id
	rec := doAssetRequestJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/asset-requests?company_id=%d&applicant_id=%d", companyID, competitorID), userToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp assetRequestAPIResp
	var page struct {
		Total int64                 `json:"total"`
		Items []model.AssetRequest  `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	json.Unmarshal(resp.Data, &page)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != mine.ID {
		t.Fatalf("user list must be self-scoped: total=%d items=%+v", page.Total, page.Items)
	}

	// 普通用户不能审批/驳回 → 403
	rec = doAssetRequestJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/asset-requests/%d/approve", mine.ID), userToken, gin.H{"company_id": companyID})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user approve must be 403, got %d", rec.Code)
	}

	// 跨人读取/撤回按不存在处理
	rec = doAssetRequestJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/asset-requests/%d?company_id=%d", mine.ID, companyID), otherToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-person get status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doAssetRequestJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/asset-requests/%d/cancel", mine.ID), otherToken, gin.H{"company_id": companyID})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-person cancel status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 本人撤回自己的申请
	rec = doAssetRequestJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/asset-requests/%d/cancel", mine.ID), userToken, gin.H{"company_id": companyID})
	if rec.Code != http.StatusOK {
		t.Fatalf("self cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
	var canceled model.AssetRequest
	json.Unmarshal(rec.Body.Bytes(), &resp)
	json.Unmarshal(resp.Data, &canceled)
	if canceled.Status != model.AssetRequestStatusCanceled {
		t.Fatalf("unexpected canceled: %+v", canceled)
	}
}

func TestAssetRequestRejectFlow(t *testing.T) {
	r := setupAssetRequestRouter(t)
	companyID, userID, _, adminID, freeAssetID, _, _ := seedAssetRequestFixture(t)
	userToken := userTokenFor(t, userID, "zhangsan", "user")
	adminToken := userTokenFor(t, adminID, "itadmin", "admin")

	created := createRequestViaAPI(t, r, userToken, companyID, freeAssetID, 0, true, "申请一台")
	rec := doAssetRequestJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/asset-requests/%d/reject", created.ID), adminToken, gin.H{
			"company_id":      companyID,
			"decision_remark": "库存紧张，下季度再议",
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("reject status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp assetRequestAPIResp
	var rejected model.AssetRequest
	json.Unmarshal(rec.Body.Bytes(), &resp)
	json.Unmarshal(resp.Data, &rejected)
	if rejected.Status != model.AssetRequestStatusRejected ||
		rejected.DecisionRemark != "库存紧张，下季度再议" ||
		rejected.ApprovedBy == nil || *rejected.ApprovedBy != adminID {
		t.Fatalf("unexpected rejected: %+v", rejected)
	}
	// 驳回不动资产，也不写履历
	asset := loadAsset(t, freeAssetID)
	if asset.UserID != nil || asset.Status != model.AssetStatusStock {
		t.Fatalf("rejected request must not touch asset: %+v", asset)
	}
	var n int64
	store.DB.Model(&model.AssetEvent{}).
		Where("asset_id = ?", freeAssetID).Count(&n)
	if n != 0 {
		t.Fatalf("reject must not record events, got %d", n)
	}

	// 撤回已驳回的申请 → 状态不允许
	rec = doAssetRequestJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/asset-requests/%d/cancel", created.ID), userToken, gin.H{"company_id": companyID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cancel rejected status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssetRequestAdminProxyCreate(t *testing.T) {
	r := setupAssetRequestRouter(t)
	companyID, userID, _, _, freeAssetID, _, _ := seedAssetRequestFixture(t)
	adminToken := userTokenFor(t, 9, "itadmin", "admin")

	// 管理员代录：applicant_id 指定员工，姓名快照取自用户表
	created := createRequestViaAPI(t, r, adminToken, companyID, freeAssetID, userID, true, "代录申请")
	if created.ApplicantID != userID || created.ApplicantName != "张三" {
		t.Fatalf("proxy create mismatch: %+v", created)
	}
	// 代录指定的申请人不存在 → 400
	rec := doAssetRequestJSON(t, r, http.MethodPost, "/api/v1/asset-requests", adminToken, gin.H{
		"company_id": companyID, "asset_id": freeAssetID,
		"applicant_id": 99999, "is_long_term": true, "reason": "代录",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown applicant status=%d body=%s", rec.Code, rec.Body.String())
	}
}
