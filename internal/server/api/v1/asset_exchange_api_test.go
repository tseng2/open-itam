package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/assetexcel"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

func setupAssetExchangeRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/asset_exchange_test.db")
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
	RegisterAssetRoutes(protected, time.Minute)
	return r
}

// exchangeSheet 用 excelize 原生构造导入文件（可控构造坏行，Export 只能产出合法行）
func exchangeSheet(t *testing.T, headers []string, rows [][]interface{}) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for i, h := range headers {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			t.Fatalf("cell name: %v", err)
		}
		if err := f.SetCellValue("Sheet1", cell, h); err != nil {
			t.Fatalf("set header: %v", err)
		}
	}
	for r, row := range rows {
		for c, v := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+2)
			if err != nil {
				t.Fatalf("cell name: %v", err)
			}
			if err := f.SetCellValue("Sheet1", cell, v); err != nil {
				t.Fatalf("set cell: %v", err)
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("write buffer: %v", err)
	}
	return buf.Bytes()
}

func importMultipart(t *testing.T, filename string, data []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func doImportRequest(t *testing.T, r http.Handler, token string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assets/import", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func seedExchangeCompany(t *testing.T) (companyID int64) {
	t.Helper()
	company := model.Company{Name: "导入导出测试公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	return company.ID
}

func TestAssetImportCreatesAssets(t *testing.T) {
	r := setupAssetExchangeRouter(t)
	companyID := seedExchangeCompany(t)
	token := adminToken(t)

	// 三年直线折旧、残值率 5%：27 个月 → 8000×(1-27/36)=2000（残值 400 不触底）
	rule := model.DepreciationRule{
		CompanyID: companyID,
		Name:      "三年直线",
		Months:    36,
		FloorType: "percent",
		FloorVal:  0.05,
		Enabled:   true,
	}
	if err := store.DB.Create(&rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	// AddDate(0,-27,0) 保证 MonthsElapsed 恰为 27（月内日不变或归一化前移，不跨整月边界）
	purchase := time.Now().AddDate(0, -27, 0)
	fileData, err := assetexcel.Export([]assetexcel.Row{
		{
			AssetTag: "IT-IMP-1", CategoryID: model.AssetCategoryNotebook, CategoryName: "笔记本电脑",
			Status: model.AssetStatusInUse, Brand: "联想", PurchaseDate: &purchase,
			OriginalPrice: 8000, SecEncrypted: true, Remark: "批量导入1",
		},
		{
			AssetTag: "IT-IMP-2", CategoryID: model.AssetCategoryPC, CategoryName: "台式整机",
			Status: model.AssetStatusStock, OriginalPrice: 5000, NetValue: 500,
		},
	})
	if err != nil {
		t.Fatalf("build import file: %v", err)
	}
	body, ct := importMultipart(t, "import.xlsx", fileData, map[string]string{
		"company_id":      fmt.Sprintf("%d", companyID),
		"depreciation_id": fmt.Sprintf("%d", rule.ID),
	})
	rec := doImportRequest(t, r, token, body, ct)
	if rec.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp dispatchAPIResp
	var payload struct {
		Created int `json:"created"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("resp decode: err=%v code=%d body=%s", err, resp.Code, rec.Body.String())
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil || payload.Created != 2 {
		t.Fatalf("created: %+v err=%v", payload, err)
	}

	var a1, a2 model.Asset
	if err := store.DB.Where("asset_tag = ?", "IT-IMP-1").First(&a1).Error; err != nil {
		t.Fatalf("query imported asset 1: %v", err)
	}
	if a1.CompanyID != companyID || a1.CategoryID != model.AssetCategoryNotebook ||
		a1.Status != model.AssetStatusInUse || a1.OriginalPrice != 8000 || !a1.SecEncrypted ||
		a1.PurchaseDate == nil {
		t.Fatalf("imported asset 1 fields: %+v", a1)
	}
	// 净值留空 + 挂规则 → 导入即按折旧口径算出净值
	if a1.NetValue != 2000 {
		t.Fatalf("rule-computed net value: want 2000, got %v", a1.NetValue)
	}
	if a1.DepreciationID == nil || *a1.DepreciationID != rule.ID {
		t.Fatalf("depreciation not attached: %+v", a1.DepreciationID)
	}

	// Excel 账面净值优先，规则不覆盖财务已录值
	if err := store.DB.Where("asset_tag = ?", "IT-IMP-2").First(&a2).Error; err != nil {
		t.Fatalf("query imported asset 2: %v", err)
	}
	if a2.NetValue != 500 {
		t.Fatalf("excel net value should win: got %v", a2.NetValue)
	}
	if a2.DepreciationID == nil || *a2.DepreciationID != rule.ID {
		t.Fatalf("depreciation not attached to all rows: %+v", a2.DepreciationID)
	}

	// 每条资产一条建账履历，操作人取自 JWT
	for _, id := range []int64{a1.ID, a2.ID} {
		var events []model.AssetEvent
		store.DB.Where("asset_id = ? AND event_type = ?", id, model.AssetEventCreate).Find(&events)
		if len(events) != 1 {
			t.Fatalf("asset %d: want 1 create event, got %d", id, len(events))
		}
		if events[0].OperatorID == nil || *events[0].OperatorID != 1 {
			t.Fatalf("event operator should be jwt user 1: %+v", events[0].OperatorID)
		}
	}
}

func TestAssetImportRejectsRowErrors(t *testing.T) {
	r := setupAssetExchangeRouter(t)
	companyID := seedExchangeCompany(t)
	token := adminToken(t)

	// 在库 + 软删除各占一个编码（asset_tag 全局唯一索引，软删也占位）
	live := model.Asset{CompanyID: companyID, CategoryID: 1, AssetTag: "IT-EXIST", Status: 10}
	if err := store.DB.Create(&live).Error; err != nil {
		t.Fatalf("seed live asset: %v", err)
	}
	dead := model.Asset{CompanyID: companyID, CategoryID: 1, AssetTag: "IT-DELETED", Status: 10}
	if err := store.DB.Create(&dead).Error; err != nil {
		t.Fatalf("seed dead asset: %v", err)
	}
	if err := store.DB.Delete(&dead).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	fileData := exchangeSheet(t,
		[]string{"资产编码", "类别", "状态"},
		[][]interface{}{
			{"IT-EXIST", "台式机", ""},  // DB 已存在
			{"IT-DELETED", "台式机", ""}, // 软删除记录仍占位
			{"", "台式机", ""},           // 编码为空
			{"IT-NEW", "台式机", ""},     // 合法（但整体拒收后不应入库）
		},
	)
	body, ct := importMultipart(t, "import.xlsx", fileData, map[string]string{
		"company_id": fmt.Sprintf("%d", companyID),
	})
	rec := doImportRequest(t, r, token, body, ct)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp dispatchAPIResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 40006 {
		t.Fatalf("resp decode: err=%v code=%d body=%s", err, resp.Code, rec.Body.String())
	}
	var payload struct {
		Errors     []assetexcel.RowError `json:"errors"`
		ErrorCount int                    `json:"error_count"`
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.ErrorCount != 3 || len(payload.Errors) != 3 {
		t.Fatalf("error count: %+v", payload)
	}
	joined := ""
	for _, e := range payload.Errors {
		joined += e.Reason
	}
	for _, want := range []string{"IT-EXIST", "IT-DELETED", "资产编码为空"} {
		if !bytes.Contains([]byte(joined), []byte(want)) {
			t.Fatalf("error reasons missing %q: %+v", want, payload.Errors)
		}
	}

	// 整体拒收：合法行也一并回滚，不产生半截导入
	var total int64
	store.DB.Model(&model.Asset{}).Where("company_id = ?", companyID).Count(&total)
	if total != 1 { // 仅 seed 的 live 资产（dead 已软删不出现在默认查询）
		t.Fatalf("no rows should be imported on reject, got total=%d", total)
	}
}

func TestAssetImportFileAndRuleValidation(t *testing.T) {
	r := setupAssetExchangeRouter(t)
	companyID := seedExchangeCompany(t)
	token := adminToken(t)

	// 缺文件 → 400
	body, ct := importMultipart(t, "", nil, map[string]string{"company_id": fmt.Sprintf("%d", companyID)})
	if rec := doImportRequest(t, r, token, body, ct); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing file status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 缺 company_id → 400
	fileData := exchangeSheet(t, []string{"资产编码", "类别"}, [][]interface{}{{"IT-X", "台式机"}})
	body, ct = importMultipart(t, "import.xlsx", fileData, nil)
	if rec := doImportRequest(t, r, token, body, ct); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing company status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 非 xlsx 内容 → 400
	body, ct = importMultipart(t, "import.xlsx", []byte("junk"), map[string]string{
		"company_id": fmt.Sprintf("%d", companyID),
	})
	if rec := doImportRequest(t, r, token, body, ct); rec.Code != http.StatusBadRequest {
		t.Fatalf("junk file status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 折旧规则不存在 / 跨公司 → 404（规则属于另一公司）
	other := model.Company{Name: "另一家公司-" + t.Name()}
	if err := store.DB.Create(&other).Error; err != nil {
		t.Fatalf("seed other company: %v", err)
	}
	foreignRule := model.DepreciationRule{CompanyID: other.ID, Name: "别人家的规则", Months: 36, FloorType: "percent", FloorVal: 0.05, Enabled: true}
	if err := store.DB.Create(&foreignRule).Error; err != nil {
		t.Fatalf("seed foreign rule: %v", err)
	}
	body, ct = importMultipart(t, "import.xlsx", fileData, map[string]string{
		"company_id":      fmt.Sprintf("%d", companyID),
		"depreciation_id": fmt.Sprintf("%d", foreignRule.ID),
	})
	if rec := doImportRequest(t, r, token, body, ct); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company rule status=%d body=%s", rec.Code, rec.Body.String())
	}
	body, ct = importMultipart(t, "import.xlsx", fileData, map[string]string{
		"company_id":      fmt.Sprintf("%d", companyID),
		"depreciation_id": "999999",
	})
	if rec := doImportRequest(t, r, token, body, ct); rec.Code != http.StatusNotFound {
		t.Fatalf("missing rule status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssetExportFiltersAndContent(t *testing.T) {
	r := setupAssetExchangeRouter(t)
	companyID := seedExchangeCompany(t)
	token := adminToken(t)

	user := model.User{CompanyID: companyID, Username: "wangwu", RealName: "王五", Role: "user"}
	if err := store.DB.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	inUse := model.Asset{
		CompanyID: companyID, CategoryID: 2, CategoryName: "笔记本电脑",
		AssetTag: "IT-EXP-1", Status: model.AssetStatusInUse, UserID: &user.ID,
		Brand: "联想", OriginalPrice: 8000,
	}
	scrapped := model.Asset{
		CompanyID: companyID, CategoryID: 1, CategoryName: "台式整机",
		AssetTag: "IT-EXP-2", Status: model.AssetStatusScrapped,
	}
	if err := store.DB.Create(&inUse).Error; err != nil {
		t.Fatalf("seed in-use asset: %v", err)
	}
	if err := store.DB.Create(&scrapped).Error; err != nil {
		t.Fatalf("seed scrapped asset: %v", err)
	}

	company := model.Company{}
	if err := store.DB.First(&company, companyID).Error; err != nil {
		t.Fatalf("reload company: %v", err)
	}

	path := fmt.Sprintf("/api/v1/assets/export?company_id=%d&status=20", companyID)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("export content type: %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Fatal("export should set Content-Disposition")
	}

	f, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("open exported xlsx: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows(assetexcel.SheetNameAssets, excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatalf("read exported rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("status=20 export should contain 1 data row, got %d", len(rows)-1)
	}
	if rows[1][0] != "IT-EXP-1" || rows[1][8] != "使用中" {
		t.Fatalf("unexpected row: %+v", rows[1])
	}
	// 领用人/公司富化列（Z/AA）
	if rows[1][25] != "王五" || rows[1][26] != company.Name {
		t.Fatalf("enriched columns: %+v", rows[1])
	}
}

func TestAssetImportTemplate(t *testing.T) {
	r := setupAssetExchangeRouter(t)
	token := adminToken(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets/import-template", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("template status=%d body=%s", rec.Code, rec.Body.String())
	}
	f, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("open template: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows(assetexcel.SheetNameAssets)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if len(rows) < 2 || rows[0][0] != "资产编码" || rows[1][0] != "IT-2024-0001" {
		t.Fatalf("unexpected template: %+v", rows)
	}
}

func TestAssetExchangeRequiresAdminRole(t *testing.T) {
	r := setupAssetExchangeRouter(t)
	token, err := middleware.GenerateToken(2, "ordinary", "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets/export", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin export status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/assets/import-template", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin template status=%d body=%s", rec.Code, rec.Body.String())
	}

	body, ct := importMultipart(t, "import.xlsx", []byte("x"), nil)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/assets/import", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin import status=%d body=%s", rec.Code, rec.Body.String())
	}
}
