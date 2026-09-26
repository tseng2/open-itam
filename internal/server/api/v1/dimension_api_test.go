package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/assetexcel"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// P1 维度治理 gin 集成测试：四组维表端点 + 资产挂接/富化/过滤联动。
// 走 InitDB + glebarez SQLite，GormStore 维度实现经此覆盖

func setupDimensionRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/dimension_api_test.db")
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
	RegisterDimensionRoutes(protected)
	// 资产挂接/富化/过滤联动走既有资产路由面
	RegisterAssetRoutes(protected)
	return r
}

func seedDimensionCompany(t *testing.T) int64 {
	t.Helper()
	company := model.Company{Name: "维度测试公司-" + t.Name() + time.Now().Format("150405.000000000")}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	return company.ID
}

func uniqueTag(prefix string) string {
	return prefix + time.Now().Format("150405.000000000")
}

// dimCreate 建维度并断言成功，返回 data（json 原文）
func dimCreate(t *testing.T, r http.Handler, token, path string, body map[string]any) map[string]any {
	t.Helper()
	rec := doDepJSON(t, r, "POST", path, token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create %s: http=%d body=%s", path, rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create resp: %v", err)
	}
	return resp.Data
}

func TestDimensionPermissionsAndManufacturerCRUD(t *testing.T) {
	r := setupDimensionRouter(t)
	admin := userToken(t, "admin")
	companyID := seedDimensionCompany(t)

	// 读面开放给所有登录用户（资产表单下拉依赖）
	rec := doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/manufacturers?company_id=%d", companyID), userToken(t, "user"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user read list must pass, got %d", rec.Code)
	}
	// 写面仅 admin
	rec = doDepJSON(t, r, "POST", "/api/v1/manufacturers", userToken(t, "user"), map[string]any{
		"company_id": companyID, "name": "联想",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin create must 403, got %d", rec.Code)
	}

	// 创建：名称 trim 落库
	created := dimCreate(t, r, admin, "/api/v1/manufacturers", map[string]any{
		"company_id": companyID, "name": "  联想  ", "remark": "Lenovo",
	})
	id := int64(created["id"].(float64))
	if created["name"] != "联想" {
		t.Fatalf("name must be trimmed, got %v", created["name"])
	}

	// 同名冲突 → 409
	rec = doDepJSON(t, r, "POST", "/api/v1/manufacturers", admin, map[string]any{
		"company_id": companyID, "name": "联想",
	})
	if rec.Code != http.StatusConflict || depRespCode(t, rec) != 40901 {
		t.Fatalf("duplicate name must 409/40901, got %d %s", rec.Code, rec.Body.String())
	}

	// 列表 keyword
	dimCreate(t, r, admin, "/api/v1/manufacturers", map[string]any{"company_id": companyID, "name": "DELL"})
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/manufacturers?company_id=%d&keyword=del&page_size=10", companyID), admin, nil)
	var list struct {
		Data struct {
			Total int64                    `json:"total"`
			Items []map[string]any         `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	if list.Data.Total != 1 || list.Data.Items[0]["name"] != "DELL" {
		t.Fatalf("keyword list: %+v", list.Data)
	}
	// 分页
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/manufacturers?company_id=%d&page=1&page_size=1", companyID), admin, nil)
	json.Unmarshal(rec.Body.Bytes(), &list)
	if list.Data.Total != 2 || len(list.Data.Items) != 1 {
		t.Fatalf("pagination: total=%d len=%d", list.Data.Total, len(list.Data.Items))
	}
	// 公司边界：别公司看不到
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/manufacturers?company_id=%d", companyID+999), admin, nil)
	json.Unmarshal(rec.Body.Bytes(), &list)
	if list.Data.Total != 0 {
		t.Fatalf("cross-company list must be empty, got %d", list.Data.Total)
	}

	// 更新：改名成功
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/manufacturers/%d", id), admin, map[string]any{
		"company_id": companyID, "name": "Lenovo 联想", "remark": "改名",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: http=%d body=%s", rec.Code, rec.Body.String())
	}
	// 越权公司更新 → 404
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/manufacturers/%d", id), admin, map[string]any{
		"company_id": companyID + 999, "name": "x",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company update must 404, got %d", rec.Code)
	}

	// 删除：不存在 404 → 成功 → 幂等 404
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/manufacturers/999999?company_id=%d", companyID), admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing must 404, got %d", rec.Code)
	}
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/manufacturers/%d?company_id=%d", id, companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: http=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/manufacturers/%d?company_id=%d", id, companyID), admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete must 404, got %d", rec.Code)
	}
}

func TestSupplierAndLocationFlow(t *testing.T) {
	r := setupDimensionRouter(t)
	admin := userToken(t, "admin")
	companyID := seedDimensionCompany(t)

	// 供应商：联系人/电话 round-trip
	sup := dimCreate(t, r, admin, "/api/v1/suppliers", map[string]any{
		"company_id": companyID, "name": "京东采购", "contact_name": "王经理", "phone": "13800000000",
	})
	if sup["contact_name"] != "王经理" || sup["phone"] != "13800000000" {
		t.Fatalf("supplier fields: %+v", sup)
	}

	// 位置库：父子挂接 + 富化 + 环/越权校验
	hq := dimCreate(t, r, admin, "/api/v1/locations", map[string]any{
		"company_id": companyID, "name": "总部大楼",
	})
	// 父节点不存在 → 404
	rec := doDepJSON(t, r, "POST", "/api/v1/locations", admin, map[string]any{
		"company_id": companyID, "name": "3F", "parent_id": 999999,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing parent must 404, got %d %s", rec.Code, rec.Body.String())
	}
	// 父节点跨公司 → 404
	otherCompany := seedDimensionCompany(t)
	otherLoc := dimCreate(t, r, admin, "/api/v1/locations", map[string]any{
		"company_id": otherCompany, "name": "别公司位置",
	})
	rec = doDepJSON(t, r, "POST", "/api/v1/locations", admin, map[string]any{
		"company_id": companyID, "name": "3F", "parent_id": otherLoc["id"],
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company parent must 404, got %d", rec.Code)
	}
	// 正常子位置
	child := dimCreate(t, r, admin, "/api/v1/locations", map[string]any{
		"company_id": companyID, "name": "3F 办公区", "parent_id": hq["id"],
	})
	// 列表富化 parent_name
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/locations?company_id=%d&page_size=10", companyID), admin, nil)
	var locList struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &locList)
	var childRow map[string]any
	for _, item := range locList.Data.Items {
		if fmt.Sprintf("%v", item["name"]) == "3F 办公区" {
			childRow = item
		}
	}
	if childRow == nil || childRow["parent_name"] != "总部大楼" {
		t.Fatalf("parent_name not enriched: %+v", childRow)
	}

	// 自引用 → 400；环 A→B→A → 400
	childID := int64(child["id"].(float64))
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/locations/%d", childID), admin, map[string]any{
		"company_id": companyID, "name": "3F 办公区", "parent_id": childID,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("self parent must 400, got %d", rec.Code)
	}
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/locations/%d", int64(hq["id"].(float64))), admin, map[string]any{
		"company_id": companyID, "name": "总部大楼", "parent_id": childID,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cycle must 400, got %d", rec.Code)
	}

	// 删除：有子位置 → 409；子位置先删 → 父可删
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/locations/%d?company_id=%d", int64(hq["id"].(float64)), companyID), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete parent with children must 409, got %d", rec.Code)
	}
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/locations/%d?company_id=%d", childID, companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete child: http=%d", rec.Code)
	}
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/locations/%d?company_id=%d", int64(hq["id"].(float64)), companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete parent after child: http=%d", rec.Code)
	}
}

func TestAssetModelCreateAndEnrich(t *testing.T) {
	r := setupDimensionRouter(t)
	admin := userToken(t, "admin")
	companyID := seedDimensionCompany(t)

	mfr := dimCreate(t, r, admin, "/api/v1/manufacturers", map[string]any{"company_id": companyID, "name": "联想"})
	rule := model.DepreciationRule{CompanyID: companyID, Name: "3年直线", Months: 36, FloorType: "percent", Enabled: true}
	if err := store.DB.Create(&rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	// 挂接厂商/折旧规则的型号
	am := dimCreate(t, r, admin, "/api/v1/asset-models", map[string]any{
		"company_id": companyID, "name": "ThinkPad X1 Carbon Gen 11", "category_id": 2,
		"manufacturer_id": mfr["id"], "depreciation_id": rule.ID, "eol_months": 60,
	})
	if am["eol_months"] != float64(60) {
		t.Fatalf("eol not persisted: %+v", am)
	}

	// 列表富化 manufacturer_name / depreciation_name；类别过滤
	rec := doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/asset-models?company_id=%d&category_id=2", companyID), admin, nil)
	var list struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Data.Items) != 1 {
		t.Fatalf("category filter: %+v", list.Data.Items)
	}
	item := list.Data.Items[0]
	if item["manufacturer_name"] != "联想" || item["depreciation_name"] != "3年直线" {
		t.Fatalf("model enrich: %+v", item)
	}

	// 跨公司厂商 → 404
	otherCompany := seedDimensionCompany(t)
	rec = doDepJSON(t, r, "POST", "/api/v1/asset-models", admin, map[string]any{
		"company_id": otherCompany, "name": "x", "manufacturer_id": mfr["id"],
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company manufacturer must 404, got %d", rec.Code)
	}
	// 同名 → 409
	rec = doDepJSON(t, r, "POST", "/api/v1/asset-models", admin, map[string]any{
		"company_id": companyID, "name": "ThinkPad X1 Carbon Gen 11",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate model must 409, got %d", rec.Code)
	}
}

func TestDimensionDeleteReferenceGuards(t *testing.T) {
	r := setupDimensionRouter(t)
	admin := userToken(t, "admin")
	companyID := seedDimensionCompany(t)

	createAsset := func(tag string, body map[string]any) int64 {
		t.Helper()
		body["company_id"] = companyID
		body["category_id"] = 2
		body["asset_tag"] = tag
		rec := doDepJSON(t, r, "POST", "/api/v1/assets", admin, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("create asset: http=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data model.Asset `json:"data"`
		}
		json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp.Data.ID
	}

	// 厂商被资产引用 → 409；清挂接后可删
	mfr := dimCreate(t, r, admin, "/api/v1/manufacturers", map[string]any{"company_id": companyID, "name": "联想"})
	assetID := createAsset(uniqueTag("AST-DIM-MFR-"), map[string]any{"manufacturer_id": mfr["id"]})
	rec := doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/manufacturers/%v?company_id=%d", mfr["id"], companyID), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete referenced manufacturer must 409, got %d", rec.Code)
	}
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/assets/%d", assetID), admin, map[string]any{"manufacturer_id": 0})
	if rec.Code != http.StatusOK {
		t.Fatalf("detach manufacturer: http=%d", rec.Code)
	}
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/manufacturers/%v?company_id=%d", mfr["id"], companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete after detach: http=%d", rec.Code)
	}

	// 厂商被型号库引用 → 409
	mfr2 := dimCreate(t, r, admin, "/api/v1/manufacturers", map[string]any{"company_id": companyID, "name": "DELL"})
	dimCreate(t, r, admin, "/api/v1/asset-models", map[string]any{
		"company_id": companyID, "name": "OptiPlex 7010", "manufacturer_id": mfr2["id"],
	})
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/manufacturers/%v?company_id=%d", mfr2["id"], companyID), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete manufacturer referenced by model must 409, got %d", rec.Code)
	}

	// 供应商 / 位置 / 型号被资产引用 → 409 → 清挂接后可删
	sup := dimCreate(t, r, admin, "/api/v1/suppliers", map[string]any{"company_id": companyID, "name": "京东采购"})
	assetID = createAsset(uniqueTag("AST-DIM-SUP-"), map[string]any{"supplier_id": sup["id"]})
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/suppliers/%v?company_id=%d", sup["id"], companyID), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete referenced supplier must 409, got %d", rec.Code)
	}
	doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/assets/%d", assetID), admin, map[string]any{"supplier_id": 0})
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/suppliers/%v?company_id=%d", sup["id"], companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete supplier after detach: http=%d", rec.Code)
	}

	loc := dimCreate(t, r, admin, "/api/v1/locations", map[string]any{"company_id": companyID, "name": "机房"})
	assetID = createAsset(uniqueTag("AST-DIM-LOC-"), map[string]any{"location_id": loc["id"]})
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/locations/%v?company_id=%d", loc["id"], companyID), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete referenced location must 409, got %d", rec.Code)
	}
	doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/assets/%d", assetID), admin, map[string]any{"location_id": 0})

	am := dimCreate(t, r, admin, "/api/v1/asset-models", map[string]any{"company_id": companyID, "name": "ThinkPad T14"})
	assetID = createAsset(uniqueTag("AST-DIM-MDL-"), map[string]any{"model_id": am["id"]})
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/asset-models/%v?company_id=%d", am["id"], companyID), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete referenced model must 409, got %d", rec.Code)
	}
	doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/assets/%d", assetID), admin, map[string]any{"model_id": 0})
	rec = doDepJSON(t, r, "DELETE", fmt.Sprintf("/api/v1/asset-models/%v?company_id=%d", am["id"], companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete model after detach: http=%d", rec.Code)
	}
}

func TestAssetDimensionLinkageAndEnrichment(t *testing.T) {
	r := setupDimensionRouter(t)
	admin := userToken(t, "admin")
	companyID := seedDimensionCompany(t)

	// 维度与规则夹具
	mfr := dimCreate(t, r, admin, "/api/v1/manufacturers", map[string]any{"company_id": companyID, "name": "联想"})
	rule := model.DepreciationRule{CompanyID: companyID, Name: "3年直线", Months: 36, FloorType: "percent", Enabled: true}
	if err := store.DB.Create(&rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	am := dimCreate(t, r, admin, "/api/v1/asset-models", map[string]any{
		"company_id": companyID, "name": "ThinkPad X1", "category_id": 2,
		"manufacturer_id": mfr["id"], "depreciation_id": rule.ID,
	})
	loc := dimCreate(t, r, admin, "/api/v1/locations", map[string]any{"company_id": companyID, "name": "机房"})
	sup := dimCreate(t, r, admin, "/api/v1/suppliers", map[string]any{"company_id": companyID, "name": "京东采购"})

	// 类别不匹配的型号挂接 → 400
	rec := doDepJSON(t, r, "POST", "/api/v1/assets", admin, map[string]any{
		"company_id": companyID, "category_id": 1, "asset_tag": uniqueTag("AST-DIM-CAT-"),
		"model_id": am["id"],
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("model category mismatch must 400, got %d %s", rec.Code, rec.Body.String())
	}

	// 全维度挂接建账：快照写回 + 折旧继承 + 富化响应
	rec = doDepJSON(t, r, "POST", "/api/v1/assets", admin, map[string]any{
		"company_id": companyID, "category_id": 2, "asset_tag": uniqueTag("AST-DIM-FULL-"),
		"model_id": am["id"], "location_id": loc["id"], "supplier_id": sup["id"],
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create with dimensions: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data model.Asset `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	linked := created.Data
	if linked.Brand != "联想" || linked.ModelName != "ThinkPad X1" || linked.Location != "机房" {
		t.Fatalf("snapshots not written: %+v", linked)
	}
	if linked.SupplierName != "京东采购" {
		t.Fatalf("supplier_name not enriched: %+v", linked.SupplierName)
	}
	if linked.DepreciationID == nil || *linked.DepreciationID != rule.ID {
		t.Fatalf("depreciation not inherited from model: %+v", linked.DepreciationID)
	}
	if linked.ManufacturerID == nil || linked.ModelID == nil || linked.LocationID == nil || linked.SupplierID == nil {
		t.Fatalf("dimension FKs not set: %+v", linked)
	}

	// 只选型号不选厂商：厂商随型号带出
	rec = doDepJSON(t, r, "POST", "/api/v1/assets", admin, map[string]any{
		"company_id": companyID, "category_id": 2, "asset_tag": uniqueTag("AST-DIM-MDLONLY-"),
		"model_id": am["id"],
	})
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Data.Brand != "联想" || created.Data.ManufacturerID == nil {
		t.Fatalf("manufacturer must be adopted from model: %+v", created.Data)
	}

	// 存量自由文本资产：无外键时快照原样展示（兼容回退）
	legacy := model.Asset{
		CompanyID: companyID, CategoryID: 2, CategoryName: "笔记本电脑",
		AssetTag: uniqueTag("AST-DIM-LEGACY-"), Brand: "旧品牌", Status: model.AssetStatusStock,
	}
	if err := store.DB.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	// 维度重命名传播：改名厂商后列表富化覆盖展示
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/manufacturers/%v", mfr["id"]), admin, map[string]any{
		"company_id": companyID, "name": "Lenovo 联想",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename manufacturer: http=%d", rec.Code)
	}
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/assets?company_id=%d&manufacturer_id=%v", companyID, mfr["id"]), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter by manufacturer: http=%d", rec.Code)
	}
	var list struct {
		Data struct {
			Total int64         `json:"total"`
			Items []model.Asset `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	if list.Data.Total != 2 || len(list.Data.Items) != 2 {
		t.Fatalf("manufacturer_id filter: total=%d", list.Data.Total)
	}
	for _, a := range list.Data.Items {
		if a.Brand != "Lenovo 联想" {
			t.Fatalf("rename must propagate to enriched brand, got %q", a.Brand)
		}
	}
	// 存量自由文本资产不受富化影响
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/assets?company_id=%d&asset_tag=%s", companyID, legacy.AssetTag), admin, nil)
	json.Unmarshal(rec.Body.Bytes(), &list)
	if list.Data.Total != 1 || list.Data.Items[0].Brand != "旧品牌" {
		t.Fatalf("legacy free-text must keep its own value: %+v", list.Data.Items)
	}

	// 清挂接：外键清空但快照文本保留
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/assets/%d", linked.ID), admin, map[string]any{"location_id": 0})
	if rec.Code != http.StatusOK {
		t.Fatalf("clear location: http=%d", rec.Code)
	}
	var updated struct {
		Data model.Asset `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Data.LocationID != nil {
		t.Fatal("location FK must be cleared")
	}
	if updated.Data.Location != "机房" {
		t.Fatalf("location snapshot must be kept, got %q", updated.Data.Location)
	}
}

// TestDimensionEdgeCases 补齐分支覆盖：参数校验 400、供应商/型号/位置的
// 更新主链路、外键 404、编辑挂接（applyAssetDimensionUpdates）与 0 值归一
func TestDimensionEdgeCases(t *testing.T) {
	r := setupDimensionRouter(t)
	admin := userToken(t, "admin")
	companyID := seedDimensionCompany(t)

	// 列表缺 company_id → 400
	rec := doDepJSON(t, r, "GET", "/api/v1/manufacturers", admin, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("list without company_id must 400, got %d", rec.Code)
	}
	// 创建缺 company_id → 400
	rec = doDepJSON(t, r, "POST", "/api/v1/manufacturers", admin, map[string]any{"name": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create without company_id must 400, got %d", rec.Code)
	}
	// id 非法 → 400
	rec = doDepJSON(t, r, "PUT", "/api/v1/manufacturers/abc", admin, map[string]any{"company_id": companyID, "name": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid id must 400, got %d", rec.Code)
	}

	// 供应商：建 → 列表 → 改联系人/电话 → 列表可见
	dimCreate(t, r, admin, "/api/v1/suppliers", map[string]any{
		"company_id": companyID, "name": "苏宁采购", "contact_name": "李经理", "phone": "13900000000",
	})
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/suppliers?company_id=%d&keyword=苏宁", companyID), admin, nil)
	var supList struct {
		Data struct {
			Total int64                    `json:"total"`
			Items []map[string]any         `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &supList)
	if supList.Data.Total != 1 || supList.Data.Items[0]["phone"] != "13900000000" {
		t.Fatalf("supplier list: %+v", supList.Data)
	}
	supID := int64(supList.Data.Items[0]["id"].(float64))
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/suppliers/%d", supID), admin, map[string]any{
		"company_id": companyID, "name": "苏宁采购", "contact_name": "赵经理", "phone": "13700000000",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("supplier update: http=%d body=%s", rec.Code, rec.Body.String())
	}

	// 型号库：建（类别 0 不限 + 厂商/规则 0 归一）→ 更新改 EOL/类别
	am := dimCreate(t, r, admin, "/api/v1/asset-models", map[string]any{
		"company_id": companyID, "name": "通用显示器", "category_id": 0,
		"manufacturer_id": 0, "depreciation_id": 0, "eol_months": 36,
	})
	if am["manufacturer_id"] != nil {
		t.Fatalf("manufacturer_id=0 must normalize to nil, got %v", am["manufacturer_id"])
	}
	amPro := dimCreate(t, r, admin, "/api/v1/asset-models", map[string]any{
		"company_id": companyID, "name": "Pro 型号", "category_id": 0,
	})
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/asset-models/%v", amPro["id"]), admin, map[string]any{
		"company_id": companyID, "name": "通用显示器 Pro", "category_id": 3, "eol_months": 48,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("asset model update: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var amUpdated struct {
		Data model.AssetModel `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &amUpdated)
	if amUpdated.Data.EOLMonths != 48 || amUpdated.Data.CategoryID != 3 || amUpdated.Data.ManufacturerID != nil {
		t.Fatalf("asset model update not applied: %+v", amUpdated.Data)
	}

	// 位置库：parent_id=0 归一为顶级 → 正常改挂父级
	warehouse := dimCreate(t, r, admin, "/api/v1/locations", map[string]any{
		"company_id": companyID, "name": "外设仓库", "parent_id": 0,
	})
	if warehouse["parent_id"] != nil {
		t.Fatalf("parent_id=0 must normalize to nil, got %v", warehouse["parent_id"])
	}
	shelf := dimCreate(t, r, admin, "/api/v1/locations", map[string]any{"company_id": companyID, "name": "A 货架"})
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/locations/%v", shelf["id"]), admin, map[string]any{
		"company_id": companyID, "name": "A 货架", "parent_id": warehouse["id"],
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("re-parent: http=%d body=%s", rec.Code, rec.Body.String())
	}

	// 台账建账：不存在的厂商 → 404；类别 0（不限）型号配任意类别放行
	rec = doDepJSON(t, r, "POST", "/api/v1/assets", admin, map[string]any{
		"company_id": companyID, "category_id": 2, "asset_tag": uniqueTag("AST-DIM-404-"),
		"manufacturer_id": 999999,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bogus manufacturer must 404, got %d", rec.Code)
	}
	rec = doDepJSON(t, r, "POST", "/api/v1/assets", admin, map[string]any{
		"company_id": companyID, "category_id": 2, "asset_tag": uniqueTag("AST-DIM-CAT0-"),
		"model_id": am["id"],
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("category-0 model must fit any category: http=%d %s", rec.Code, rec.Body.String())
	}
	var cat0Asset struct {
		Data model.Asset `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &cat0Asset)

	// 台账编辑挂接：bogus 型号 → 404；类别不匹配 → 400（含同请求改类别的生效口径）
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/assets/%d", cat0Asset.Data.ID), admin, map[string]any{
		"model_id": 999999,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bogus model on update must 404, got %d", rec.Code)
	}
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/assets/%d", cat0Asset.Data.ID), admin, map[string]any{
		"model_id": amPro["id"], "category_id": 2,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("category mismatch on update must 400, got %d", rec.Code)
	}
	// 同请求把类别改成 3：与型号类别一致 → 放行（本次生效类别口径）
	mfr := dimCreate(t, r, admin, "/api/v1/manufacturers", map[string]any{"company_id": companyID, "name": "戴尔"})
	rec = doDepJSON(t, r, "PUT", fmt.Sprintf("/api/v1/assets/%d", cat0Asset.Data.ID), admin, map[string]any{
		"model_id": amPro["id"], "category_id": 3, "manufacturer_id": mfr["id"], "supplier_id": supID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("attach model/manufacturer/supplier on update: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var updated struct {
		Data model.Asset `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Data.ModelName != "通用显示器 Pro" || updated.Data.Brand != "戴尔" || updated.Data.SupplierName != "苏宁采购" {
		t.Fatalf("update attach not applied: %+v", updated.Data)
	}
	if updated.Data.ModelID == nil || updated.Data.ManufacturerID == nil || updated.Data.SupplierID == nil {
		t.Fatalf("update FKs not set: %+v", updated.Data)
	}
}

func TestExportEnrichedWithDimensions(t *testing.T) {
	r := setupDimensionRouter(t)
	admin := userToken(t, "admin")
	companyID := seedDimensionCompany(t)

	mfr := dimCreate(t, r, admin, "/api/v1/manufacturers", map[string]any{"company_id": companyID, "name": "戴尔"})
	am := dimCreate(t, r, admin, "/api/v1/asset-models", map[string]any{
		"company_id": companyID, "name": "OptiPlex 7000", "category_id": 1, "manufacturer_id": mfr["id"],
	})
	rec := doDepJSON(t, r, "POST", "/api/v1/assets", admin, map[string]any{
		"company_id": companyID, "category_id": 1, "asset_tag": uniqueTag("AST-DIM-EXP-"),
		"manufacturer_id": mfr["id"], "model_id": am["id"], "brand": "乱写的品牌",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create asset: http=%d", rec.Code)
	}

	// 导出文件与列表同口径：外键富化覆盖自由文本
	rec = doDepJSON(t, r, "GET", fmt.Sprintf("/api/v1/assets/export?company_id=%d", companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: http=%d", rec.Code)
	}
	rows, rowErrs, err := assetexcel.Parse(rec.Body.Bytes())
	if err != nil || len(rowErrs) != 0 {
		t.Fatalf("parse exported file: err=%v rowErrs=%v", err, rowErrs)
	}
	if len(rows) != 1 || rows[0].Brand != "戴尔" || rows[0].ModelName != "OptiPlex 7000" {
		t.Fatalf("export must carry governed names: %+v", rows)
	}
}
