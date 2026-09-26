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

func setupDepreciationRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/depreciation_api_test.db")
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
	RegisterDepreciationRoutes(protected)
	// 资产销账挂在资产路由面，一并注册以覆盖混组前缀
	RegisterAssetRoutes(protected)
	return r
}

func userToken(t *testing.T, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(1, "tester", role)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

func seedDepreciationFixture(t *testing.T) (companyID int64, assetID int64) {
	t.Helper()
	company := model.Company{Name: "折旧API测试公司-" + t.Name() + time.Now().Format("150405.000000000")}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	asset := model.Asset{
		CompanyID:    company.ID,
		CategoryID:   2,
		CategoryName: "笔记本",
		AssetTag:     "AST-DEP-API-" + time.Now().Format("150405.000000000"),
		Status:       model.AssetStatusInUse,
	}
	if err := store.DB.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	return company.ID, asset.ID
}

func doDepJSON(t *testing.T, r http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doDispatchJSON(t, r, method, path, token, body)
}

func depRespCode(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var resp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v body=%s", err, rec.Body.String())
	}
	return resp.Code
}

const apiLadder = `[{"period":12,"unit":"MONTH","ratio":0.35},{"period":12,"unit":"MONTH","ratio":0.30},{"period":12,"unit":"MONTH","ratio":0.30}]`

func TestDepreciationRuleCRUDFlow(t *testing.T) {
	r := setupDepreciationRouter(t)
	admin := userToken(t, "admin")
	companyID, _ := seedDepreciationFixture(t)

	// 创建：阶梯合法
	body := map[string]any{
		"company_id": companyID, "name": "IT设备3年加速折旧",
		"months": 36, "floor_type": "percent", "floor_val": 0.05,
		"stages": apiLadder, "enabled": false,
	}
	rec := doDepJSON(t, r, "POST", "/api/v1/depreciations", admin, body)
	if rec.Code != http.StatusOK || depRespCode(t, rec) != 0 {
		t.Fatalf("create rule: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data model.DepreciationRule `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Data.ID == 0 {
		t.Fatal("expected rule id")
	}
	// 停用规则必须原样落库（bool false 不允许被缺省值吞掉）
	if created.Data.Enabled {
		t.Fatal("enabled=false must persist on create")
	}

	// 列表：user 角色可读（资产表单下拉依赖）
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/depreciations?company_id=%d", companyID), userToken(t, "user"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user read list: http=%d", rec.Code)
	}
	var list struct {
		Data struct {
			Total int64                     `json:"total"`
			Items []model.DepreciationRule  `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	if list.Data.Total != 1 || len(list.Data.Items) != 1 {
		t.Fatalf("list should contain 1 rule, got total=%d", list.Data.Total)
	}

	// 详情：公司边界
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/depreciations/%d?company_id=%d", created.Data.ID, companyID+999), admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company get must 404, got %d", rec.Code)
	}

	// 更新：改直线折旧并启用
	body["name"] = "直线3年"
	body["stages"] = ""
	body["enabled"] = true
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/depreciations/%d", created.Data.ID), admin, body)
	if rec.Code != http.StatusOK || depRespCode(t, rec) != 0 {
		t.Fatalf("update rule: http=%d body=%s", rec.Code, rec.Body.String())
	}

	// 校验拒绝：stages 覆盖月数与 months 不一致
	bad := map[string]any{"company_id": companyID, "name": "x", "months": 24, "floor_type": "percent", "floor_val": 0, "stages": apiLadder}
	rec = doDepJSON(t, r, "POST", "/api/v1/depreciations", admin, bad)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("stage/months mismatch must 400, got %d", rec.Code)
	}

	// 删除：无引用 → 成功；幂等二次 → 404
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/depreciations/%d?company_id=%d", created.Data.ID, companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete rule: http=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/depreciations/%d?company_id=%d", created.Data.ID, companyID), admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete must 404, got %d", rec.Code)
	}
}

func TestDepreciationWriteRequiresAdmin(t *testing.T) {
	r := setupDepreciationRouter(t)
	companyID, _ := seedDepreciationFixture(t)

	rec := doDepJSON(t, r, "POST", "/api/v1/depreciations", userToken(t, "user"), map[string]any{
		"company_id": companyID, "name": "x", "months": 36, "floor_type": "percent", "floor_val": 0,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin create must 403, got %d", rec.Code)
	}
	rec = doDepJSON(t, r, "POST", "/api/v1/depreciations/recalculate", userToken(t, "user"), nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin recalculate must 403, got %d", rec.Code)
	}
}

func TestDeleteRuleInUseConflict(t *testing.T) {
	r := setupDepreciationRouter(t)
	admin := userToken(t, "admin")
	companyID, assetID := seedDepreciationFixture(t)

	rec := doDepJSON(t, r, "POST", "/api/v1/depreciations", admin, map[string]any{
		"company_id": companyID, "name": "在用规则", "months": 36, "floor_type": "percent", "floor_val": 0.05,
	})
	var created struct {
		Data model.DepreciationRule `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)

	// 资产挂接规则（走既有资产更新 API，覆盖 depreciation_id 字段链路）
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/assets/%d", assetID), admin, map[string]any{
		"depreciation_id": created.Data.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("attach rule to asset: http=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/depreciations/%d?company_id=%d", created.Data.ID, companyID), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete in-use rule must 409, got %d body=%s", rec.Code, rec.Body.String())
	}

	// 解除挂接（0 = 清空）后可删
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/assets/%d", assetID), admin, map[string]any{
		"depreciation_id": 0,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("detach rule: http=%d", rec.Code)
	}
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/depreciations/%d?company_id=%d", created.Data.ID, companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete after detach: http=%d", rec.Code)
	}
}

func TestRecalculateRefreshesAssetNetValue(t *testing.T) {
	r := setupDepreciationRouter(t)
	admin := userToken(t, "admin")
	companyID, assetID := seedDepreciationFixture(t)

	rec := doDepJSON(t, r, "POST", "/api/v1/depreciations", admin, map[string]any{
		"company_id": companyID, "name": "3年加速", "months": 36, "floor_type": "percent", "floor_val": 0.05,
		"stages": apiLadder,
	})
	var created struct {
		Data model.DepreciationRule `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)

	purchase := time.Now().UTC().AddDate(0, -30, 0)
	if err := store.DB.Model(&model.Asset{}).Where("id = ?", assetID).Updates(map[string]any{
		"depreciation_id": created.Data.ID,
		"purchase_date":   purchase,
		"original_price":  10000,
		"net_value":       0,
	}).Error; err != nil {
		t.Fatalf("prepare asset: %v", err)
	}

	rec = doDepJSON(t, r, "POST", "/api/v1/depreciations/recalculate", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("recalculate: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Updated int `json:"updated"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Updated != 1 {
		t.Fatalf("expected 1 updated, got %d", resp.Data.Updated)
	}

	var asset model.Asset
	store.DB.First(&asset, assetID)
	if asset.NetValue != 2000 { // 30 个月阶梯累计 0.8
		t.Fatalf("net value = %v, want 2000", asset.NetValue)
	}

	// 幂等：再次重算无变更
	rec = doDepJSON(t, r, "POST", "/api/v1/depreciations/recalculate", admin, nil)
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Updated != 0 {
		t.Fatalf("second recalc should update 0, got %d", resp.Data.Updated)
	}
}

func TestOffBookAndRestoreFlow(t *testing.T) {
	r := setupDepreciationRouter(t)
	admin := userToken(t, "admin")
	companyID, assetID := seedDepreciationFixture(t)

	offBook := func() *httptest.ResponseRecorder {
		return doDepJSON(t, r, "POST", fmt.Sprintf("/api/v1/assets/%d/off-book", assetID), admin, map[string]any{
			"company_id": companyID,
		})
	}

	// 权限：user 不可销账
	rec := doDepJSON(t, r, "POST", fmt.Sprintf("/api/v1/assets/%d/off-book", assetID), userToken(t, "user"), map[string]any{"company_id": companyID})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin off-book must 403, got %d", rec.Code)
	}

	// 销账成功：off_book 置位 + 履历留痕
	rec = offBook()
	if rec.Code != http.StatusOK {
		t.Fatalf("off-book: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.Asset `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Data.OffBook || resp.Data.OffBookAt == nil {
		t.Fatalf("asset must be off-book with date: %+v", resp.Data.OffBookAt)
	}
	if n := countAssetEvents(t, assetID, model.AssetEventOffBook); n != 1 {
		t.Fatalf("expected 1 off_book event, got %d", n)
	}

	// 重复销账 → 409
	if rec = offBook(); rec.Code != http.StatusConflict {
		t.Fatalf("double off-book must 409, got %d", rec.Code)
	}

	// 列表筛选：off_book=true 只见列管资产
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/assets?company_id=%d&off_book=true", companyID), admin, nil)
	var list struct {
		Data struct {
			Total int64          `json:"total"`
			Items []model.Asset  `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	if list.Data.Total != 1 {
		t.Fatalf("off_book filter should find 1, got %d", list.Data.Total)
	}

	// 恢复在册：标志清空 + 履历
	rec = doDepJSON(t, r, "POST", fmt.Sprintf("/api/v1/assets/%d/restore-book", assetID), admin, map[string]any{
		"company_id": companyID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("restore-book: http=%d body=%s", rec.Code, rec.Body.String())
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.OffBook || resp.Data.OffBookAt != nil {
		t.Fatal("asset must be back on book")
	}
	if n := countAssetEvents(t, assetID, model.AssetEventOffBookRestore); n != 1 {
		t.Fatalf("expected 1 restore event, got %d", n)
	}
	// 未销账时恢复 → 409
	rec = doDepJSON(t, r, "POST", fmt.Sprintf("/api/v1/assets/%d/restore-book", assetID), admin, map[string]any{"company_id": companyID})
	if rec.Code != http.StatusConflict {
		t.Fatalf("restore without off-book must 409, got %d", rec.Code)
	}

	// 跨公司销账 → 404
	rec = doDepJSON(t, r, "POST", fmt.Sprintf("/api/v1/assets/%d/off-book", assetID), admin, map[string]any{"company_id": companyID + 999})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company off-book must 404, got %d", rec.Code)
	}

	// 已报废资产销账 → 400
	if err := store.DB.Model(&model.Asset{}).Where("id = ?", assetID).
		Update("status", model.AssetStatusScrapped).Error; err != nil {
		t.Fatalf("scrap asset: %v", err)
	}
	rec = offBook()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("scrapped asset off-book must 400, got %d", rec.Code)
	}
}
