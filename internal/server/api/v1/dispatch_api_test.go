package v1

import (
	"bytes"
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

type dispatchAPIResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type dispatchPageData struct {
	Total int64                 `json:"total"`
	Items []model.AssetDispatch `json:"items"`
}

func setupDispatchRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/dispatch_api_test.db")
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
	RegisterDispatchRoutes(protected)
	return r
}

func seedDispatchFixture(t *testing.T) (companyID, assetID int64) {
	t.Helper()
	company := model.Company{Name: "外派测试公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	asset := model.Asset{
		CompanyID:    company.ID,
		CategoryID:   2,
		CategoryName: "笔记本",
		AssetTag:     "AST-DISPATCH-" + time.Now().Format("150405.000000"),
		Status:       20,
	}
	if err := store.DB.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	return company.ID, asset.ID
}

func doDispatchJSON(t *testing.T, r http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func adminToken(t *testing.T) string {
	t.Helper()
	token, err := middleware.GenerateToken(1, "tester", "admin")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

func decodeDispatchResp(t *testing.T, rec *httptest.ResponseRecorder) model.AssetDispatch {
	t.Helper()
	var resp dispatchAPIResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("resp decode: err=%v code=%d body=%s", err, resp.Code, rec.Body.String())
	}
	var d model.AssetDispatch
	if err := json.Unmarshal(resp.Data, &d); err != nil {
		t.Fatalf("decode dispatch data: %v", err)
	}
	return d
}

func countAssetEvents(t *testing.T, assetID int64, eventType string) int {
	t.Helper()
	var events []model.AssetEvent
	store.DB.Where("asset_id = ? AND event_type = ?", assetID, eventType).Find(&events)
	return len(events)
}

func TestDispatchCreateListReturnFlow(t *testing.T) {
	r := setupDispatchRouter(t)
	companyID, assetID := seedDispatchFixture(t)
	token := adminToken(t)

	createBody := gin.H{
		"company_id":         companyID,
		"asset_id":           assetID,
		"borrower_name":      "张三",
		"destination":        "深圳客户现场",
		"expected_return_at": time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339),
		"isolation_offline":  true,
		"expect_wipe":        true,
	}
	rec := doDispatchJSON(t, r, http.MethodPost, "/api/v1/dispatches", token, createBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	created := decodeDispatchResp(t, rec)
	if created.Status != model.DispatchStatusActive || !created.IsolationOffline || !created.ExpectWipe {
		t.Fatalf("unexpected created record: %+v", created)
	}

	// 履历联动：外派登记必须生成 dispatch AssetEvent，带负责人与预计归期
	if n := countAssetEvents(t, assetID, model.AssetEventDispatch); n != 1 {
		t.Fatalf("expected 1 dispatch event, got %d", n)
	}
	var event model.AssetEvent
	store.DB.Where("asset_id = ? AND event_type = ?", assetID, model.AssetEventDispatch).First(&event)
	if event.TargetPerson != "张三" || event.ReturnDate == nil {
		t.Fatalf("dispatch event incomplete: %+v", event)
	}

	// 重复登记 → 409 业务错误
	rec = doDispatchJSON(t, r, http.MethodPost, "/api/v1/dispatches", token, createBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 列表过滤：按资产查询外派中记录
	listPath := fmt.Sprintf("/api/v1/dispatches?company_id=%d&asset_id=%d&status=10", companyID, assetID)
	rec = doDispatchJSON(t, r, http.MethodGet, listPath, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp dispatchAPIResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("list resp decode: err=%v code=%d", err, resp.Code)
	}
	var page dispatchPageData
	if err := json.Unmarshal(resp.Data, &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("unexpected page result: total=%d items=%+v", page.Total, page.Items)
	}

	// 归还 → 20，并生成 dispatch_return 履历
	rec = doDispatchJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/dispatches/%d/return", created.ID), token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusOK {
		t.Fatalf("return status=%d body=%s", rec.Code, rec.Body.String())
	}
	returned := decodeDispatchResp(t, rec)
	if returned.Status != model.DispatchStatusReturned || returned.ReturnedAt == nil {
		t.Fatalf("unexpected returned record: %+v", returned)
	}
	if n := countAssetEvents(t, assetID, model.AssetEventDispatchReturn); n != 1 {
		t.Fatalf("expected 1 dispatch_return event, got %d", n)
	}

	// 已归还再归还 → 400 状态错误
	rec = doDispatchJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/dispatches/%d/return", created.ID), token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("double return status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDispatchRejectsCrossCompanyAndMissingAsset(t *testing.T) {
	r := setupDispatchRouter(t)
	companyID, assetID := seedDispatchFixture(t)
	token := adminToken(t)

	// 资产不属于该公司 → 404
	rec := doDispatchJSON(t, r, http.MethodPost, "/api/v1/dispatches", token, gin.H{
		"company_id":         companyID + 100,
		"asset_id":           assetID,
		"borrower_name":      "张三",
		"destination":        "上海",
		"expected_return_at": time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339),
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company create status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 正常登记后，跨公司归还/作废 → 404
	rec = doDispatchJSON(t, r, http.MethodPost, "/api/v1/dispatches", token, gin.H{
		"company_id":         companyID,
		"asset_id":           assetID,
		"borrower_name":      "张三",
		"destination":        "上海",
		"expected_return_at": time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339),
	})
	created := decodeDispatchResp(t, rec)

	rec = doDispatchJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/dispatches/%d/return", created.ID), token, gin.H{"company_id": companyID + 100})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company return status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doDispatchJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/dispatches/%d/cancel", created.ID), token, gin.H{"company_id": companyID + 100})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDispatchRequiresAdminRole(t *testing.T) {
	r := setupDispatchRouter(t)
	token, err := middleware.GenerateToken(2, "ordinary", "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	rec := doDispatchJSON(t, r, http.MethodPost, "/api/v1/dispatches", token, gin.H{
		"company_id":         1,
		"asset_id":           1,
		"borrower_name":      "张三",
		"destination":        "上海",
		"expected_return_at": time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339),
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin create status=%d body=%s", rec.Code, rec.Body.String())
	}
}
