package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// ---- 测试脚手架（沿用 dispatch_api_test 范式：InitDB + glebarez + httptest）----

type stocktakeAPIResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type stocktakePageData struct {
	Total int64                `json:"total"`
	Items []stocktakeListRow   `json:"items"`
}

func setupStocktakeRouter(t *testing.T) *gin.Engine {
	return setupStocktakeRouterWithLimit(t, middleware.DefaultPublicRateLimitPerMinute)
}

func setupStocktakeRouterWithLimit(t *testing.T, perMinute int) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/stocktake_api_test.db")
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
	RegisterStocktakeRoutes(protected)
	RegisterStocktakePublicRoutesWithLimit(apiV1, perMinute)
	return r
}

// seedStocktakeFixture 建公司 + 三资产（一报废验证圈定排除）
func seedStocktakeFixture(t *testing.T) (companyID int64, assetIDs []int64, tags []string) {
	t.Helper()
	company := model.Company{Name: "盘点测试公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	suffix := time.Now().Format("150405.000000")
	tags = []string{
		"AST-STK-A-" + suffix,
		"AST-STK-B-" + suffix,
		"AST-STK-C-" + suffix,
	}
	for i, tag := range tags {
		asset := model.Asset{
			CompanyID:    company.ID,
			CategoryID:   2,
			CategoryName: "笔记本",
			AssetTag:     tag,
			Brand:        "DELL",
			ModelName:    "Latitude 5440",
			SerialNumber: fmt.Sprintf("SN-%s-%d", suffix, i),
			Location:     "深圳办公室",
			ManagerName:  "资产负责人甲",
			Status:       model.AssetStatusInUse,
			Remark:       "台账备注（不得泄露到公开面）",
			OriginalPrice: 9999.99,
		}
		if err := store.DB.Create(&asset).Error; err != nil {
			t.Fatalf("seed asset: %v", err)
		}
		assetIDs = append(assetIDs, asset.ID)
	}
	// 报废资产不入盘点范围
	scrapped := model.Asset{
		CompanyID: company.ID, CategoryID: 1, CategoryName: "台式机",
		AssetTag: "AST-STK-SCRAPPED-" + suffix, Status: model.AssetStatusScrapped,
	}
	if err := store.DB.Create(&scrapped).Error; err != nil {
		t.Fatalf("seed scrapped asset: %v", err)
	}
	return company.ID, assetIDs, tags
}

func doStocktakeJSON(t *testing.T, r http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	return doDispatchJSON(t, r, method, path, token, body)
}

func createStocktakeViaAPI(t *testing.T, r *gin.Engine, companyID int64, name string, assetIDs []int64) stocktakeListRow {
	t.Helper()
	rec := doStocktakeJSON(t, r, http.MethodPost, "/api/v1/stocktakes", adminToken(t), gin.H{
		"company_id": companyID,
		"name":       name,
		"asset_ids":  assetIDs,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create stocktake status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp stocktakeAPIResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	var row stocktakeListRow
	if err := json.Unmarshal(resp.Data, &row); err != nil {
		t.Fatalf("decode created stocktake: %v", err)
	}
	return row
}

func startStocktakeViaAPI(t *testing.T, r *gin.Engine, companyID, id int64) string {
	t.Helper()
	rec := doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/start", id), adminToken(t), gin.H{"company_id": companyID})
	if rec.Code != http.StatusOK {
		t.Fatalf("start stocktake status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int    `json:"code"`
		Data struct {
			ScanToken string `json:"scan_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Data.ScanToken == "" {
		t.Fatalf("start resp decode: err=%v body=%s", err, rec.Body.String())
	}
	return resp.Data.ScanToken
}

func countStocktakeEvents(t *testing.T, assetID int64, eventType string) int64 {
	t.Helper()
	var n int64
	store.DB.Model(&model.AssetEvent{}).
		Where("asset_id = ? AND event_type = ?", assetID, eventType).Count(&n)
	return n
}

func seedPendingHardwareChange(t *testing.T, assetID int64) {
	t.Helper()
	event := model.AssetEvent{
		AssetID:      assetID,
		EventType:    model.AssetEventHardwareChange,
		Title:        "硬件变更：内存升级",
		ReviewStatus: model.AssetEventReviewPending,
	}
	if err := store.DB.Create(&event).Error; err != nil {
		t.Fatalf("seed hardware_change event: %v", err)
	}
}

// ---- 管理端 ----

func TestStocktakeAdminCreateStartCheckFinishFlow(t *testing.T) {
	r := setupStocktakeRouter(t)
	companyID, assetIDs, tags := seedStocktakeFixture(t)
	token := adminToken(t)

	// 创建：显式圈定两台，报废资产即使在 asset_ids 里也不会入范围
	created := createStocktakeViaAPI(t, r, companyID, "2026 Q4 抽盘", assetIDs[:2])
	if created.Status != model.StocktakeStatusDraft || created.ItemTotal != 2 {
		t.Fatalf("unexpected created: %+v", created)
	}

	// 详情：五段位计数就位
	rec := doStocktakeJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/stocktakes/%d?company_id=%d", created.ID, companyID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 草稿不能核对
	rec = doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/items", created.ID), token, gin.H{
			"company_id": companyID,
			"checks":     []gin.H{{"asset_tag": tags[0], "result": model.StocktakeItemNormal}},
		})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("check on draft status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 开始：返回一次性盘点码
	scanToken := startStocktakeViaAPI(t, r, companyID, created.ID)

	// 列表富化：item_total/item_checked
	rec = doStocktakeJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/stocktakes?company_id=%d&status=%d", companyID, model.StocktakeStatusProcessing), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page stocktakePageData
	var resp stocktakeAPIResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	json.Unmarshal(resp.Data, &page)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ItemTotal != 2 {
		t.Fatalf("unexpected page: %+v", page)
	}

	// A3 协同前置：给两台资产各埋一条待审 hardware_change
	seedPendingHardwareChange(t, assetIDs[0])
	seedPendingHardwareChange(t, assetIDs[1])

	// 管理端核对：一正常一丢失
	rec = doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/items", created.ID), token, gin.H{
			"company_id": companyID,
			"checks": []gin.H{
				{"item_id": 0, "asset_tag": tags[0], "result": model.StocktakeItemNormal, "actual_location": "工位B-12"},
				{"asset_tag": tags[1], "result": model.StocktakeItemLost, "remark": "现场找不到"},
			},
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("check status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 正常核对自动确认 hardware_change（A3 协同红利）；丢失留 stocktake 履历
	if n := countStocktakeEvents(t, assetIDs[0], model.AssetEventHardwareChange); n != 1 {
		t.Fatalf("normal asset should keep 1 hardware_change event, got %d", n)
	}
	var reviewStatus int
	store.DB.Model(&model.AssetEvent{}).
		Where("asset_id = ? AND event_type = ?", assetIDs[0], model.AssetEventHardwareChange).
		Select("review_status").Scan(&reviewStatus)
	if reviewStatus != model.AssetEventReviewDone {
		t.Fatalf("normal check must auto-confirm pending hardware_change, got review_status=%d", reviewStatus)
	}
	if n := countStocktakeEvents(t, assetIDs[1], model.AssetEventStocktake); n != 1 {
		t.Fatalf("lost asset must record stocktake event, got %d", n)
	}

	// 明细过滤：丢失清单
	rec = doStocktakeJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/stocktakes/%d/items?company_id=%d&result=%d", created.ID, companyID, model.StocktakeItemLost), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list items status=%d body=%s", rec.Code, rec.Body.String())
	}
	var items struct {
		Total int64                   `json:"total"`
		Items []model.StocktakeItem   `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	json.Unmarshal(resp.Data, &items)
	if items.Total != 1 || len(items.Items) != 1 || items.Items[0].AssetTag != tags[1] {
		t.Fatalf("unexpected lost items: %+v", items)
	}
	// 预载资产供表格展示
	if items.Items[0].Asset == nil || items.Items[0].Asset.Brand != "DELL" {
		t.Fatalf("asset not preloaded: %+v", items.Items[0].Asset)
	}

	// 结束盘点 → 再核对 400、盘点码失效
	rec = doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/finish", created.ID), token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusOK {
		t.Fatalf("finish status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/items", created.ID), token, gin.H{
			"company_id": companyID,
			"checks":     []gin.H{{"asset_tag": tags[0], "result": model.StocktakeItemNormal}},
		})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("check after finish status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := store.NewGormStore(store.DB).GetStocktakeByToken(t.Context(), scanToken); err == nil {
		t.Fatal("scan token must be invalidated after finish")
	}
}

func TestStocktakeAdminFullScopeAndValidation(t *testing.T) {
	r := setupStocktakeRouter(t)
	companyID, assetIDs, _ := seedStocktakeFixture(t)
	token := adminToken(t)

	// 全量圈定：3 台可盘（报废不入范围）
	created := createStocktakeViaAPI(t, r, companyID, "全量盘点", nil)
	if created.ItemTotal != 3 {
		t.Fatalf("full scope must snapshot 3 non-scrapped assets, got %d", created.ItemTotal)
	}
	_ = assetIDs

	// 条件圈定匹配 0 台 → 400
	rec := doStocktakeJSON(t, r, http.MethodPost, "/api/v1/stocktakes", token, gin.H{
		"company_id":      companyID,
		"name":            "空范围",
		"location_keyword": "不存在的机房",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty scope status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 缺名 → 400
	rec = doStocktakeJSON(t, r, http.MethodPost, "/api/v1/stocktakes", token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 草稿不能直接结束（必须先开始）
	rec = doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/finish", created.ID), token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("finish draft status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStocktakeAdminCrossCompanyAndRoleGuard(t *testing.T) {
	r := setupStocktakeRouter(t)
	companyID, assetIDs, _ := seedStocktakeFixture(t)
	token := adminToken(t)

	created := createStocktakeViaAPI(t, r, companyID, "边界任务", assetIDs[:1])

	// 跨公司：读取/开始/取消一律 404
	rec := doStocktakeJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/stocktakes/%d?company_id=%d", created.ID, companyID+100), token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company get status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/start", created.ID), token, gin.H{"company_id": companyID + 100})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company start status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 普通用户 → 403
	userToken, err := middleware.GenerateToken(9, "ordinary", "user")
	if err != nil {
		t.Fatalf("generate user token: %v", err)
	}
	rec = doStocktakeJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/stocktakes?company_id=%d", companyID), userToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin list status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ---- 免登录公开面 ----

func TestStocktakePublicScanFlow(t *testing.T) {
	r := setupStocktakeRouter(t)
	companyID, assetIDs, tags := seedStocktakeFixture(t)
	token := adminToken(t)

	created := createStocktakeViaAPI(t, r, companyID, "扫码盘点", assetIDs[:2])
	scanToken := startStocktakeViaAPI(t, r, companyID, created.ID)

	// 无效盘点码：一律 404，不区分"任务不存在/已结束"
	rec := doStocktakeJSON(t, r, http.MethodGet, "/api/v1/public/stocktakes/deadbeef", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bad token status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 概要
	rec = doStocktakeJSON(t, r, http.MethodGet, "/api/v1/public/stocktakes/"+scanToken, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp stocktakeAPIResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	var summary publicStocktakeSummary
	json.Unmarshal(resp.Data, &summary)
	if summary.Name != "扫码盘点" || summary.Counts[model.StocktakeItemPending] != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	// 范围内资产：白名单字段，且不得含价格/备注等敏感字段
	rec = doStocktakeJSON(t, r, http.MethodGet,
		"/api/v1/public/stocktakes/"+scanToken+"/assets/"+tags[0], "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, banned := range []string{"original_price", "9999.99", "台账备注"} {
		if strings.Contains(body, banned) {
			t.Fatalf("public asset view leaked sensitive field %q: %s", banned, body)
		}
	}

	// 范围外资产：只回 tag 与 in_scope=false，不泄露品牌/位置等
	rec = doStocktakeJSON(t, r, http.MethodGet,
		"/api/v1/public/stocktakes/"+scanToken+"/assets/"+tags[2], "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("out-of-scope asset status=%d body=%s", rec.Code, rec.Body.String())
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	var view publicStocktakeAssetResp
	json.Unmarshal(resp.Data, &view)
	if view.InScope || view.Asset.AssetTag != tags[2] || view.Asset.Brand != "" || view.Item != nil {
		t.Fatalf("out-of-scope response leaked data: %+v", view)
	}

	// 不存在的资产 → 404
	rec = doStocktakeJSON(t, r, http.MethodGet,
		"/api/v1/public/stocktakes/"+scanToken+"/assets/AST-NOT-EXIST", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown asset status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 扫码核对：正常 → 自动确认 hardware_change（A3 协同在公开面同样生效）
	seedPendingHardwareChange(t, assetIDs[0])
	rec = doStocktakeJSON(t, r, http.MethodPost, "/api/v1/public/stocktakes/"+scanToken+"/check", "", gin.H{
		"scanned_by": "盘点员乙",
		"checks":     []gin.H{{"asset_tag": tags[0], "result": model.StocktakeItemNormal, "actual_location": "工位C-03"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("public check status=%d body=%s", rec.Code, rec.Body.String())
	}
	var reviewStatus int
	store.DB.Model(&model.AssetEvent{}).
		Where("asset_id = ? AND event_type = ?", assetIDs[0], model.AssetEventHardwareChange).
		Select("review_status").Scan(&reviewStatus)
	if reviewStatus != model.AssetEventReviewDone {
		t.Fatalf("public normal check must auto-confirm hardware_change, got %d", reviewStatus)
	}

	// 缺盘点人 → 400
	rec = doStocktakeJSON(t, r, http.MethodPost, "/api/v1/public/stocktakes/"+scanToken+"/check", "", gin.H{
		"checks": []gin.H{{"asset_tag": tags[1], "result": model.StocktakeItemNormal}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing scanned_by status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 结束任务后盘点码即刻失效（finish/cancel 即失效）
	rec = doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/finish", created.ID), token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusOK {
		t.Fatalf("finish status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStocktakeJSON(t, r, http.MethodGet, "/api/v1/public/stocktakes/"+scanToken, "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("token after finish status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStocktakePublicRateLimit(t *testing.T) {
	r := setupStocktakeRouterWithLimit(t, 3)

	// 无需有效任务：限流在鉴权之前生效，第 4 次请求即 429
	var lastStatus int
	for i := 0; i < 4; i++ {
		rec := doStocktakeJSON(t, r, http.MethodGet, "/api/v1/public/stocktakes/whatever", "", nil)
		lastStatus = rec.Code
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exceeding limit, got %d", lastStatus)
	}
}

func TestStocktakeLabelsPDF(t *testing.T) {
	r := setupStocktakeRouter(t)
	companyID, _, tags := seedStocktakeFixture(t)
	token := adminToken(t)

	created := createStocktakeViaAPI(t, r, companyID, "标签任务", nil)

	// 按任务明细整批打印：application/pdf 且 %PDF 魔数
	rec := doStocktakeJSON(t, r, http.MethodPost, "/api/v1/stocktakes/labels", token, gin.H{
		"company_id":   companyID,
		"stocktake_id": created.ID,
		"base_url":     "http://itam.local",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("labels status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/pdf") {
		t.Fatalf("unexpected content-type: %s", ct)
	}
	if body := rec.Body.String(); !strings.HasPrefix(body, "%PDF") {
		t.Fatalf("not a PDF document: %q", body[:8])
	}

	// 手选资产编码打印
	rec = doStocktakeJSON(t, r, http.MethodPost, "/api/v1/stocktakes/labels", token, gin.H{
		"company_id": companyID,
		"asset_tags": tags[:1],
		"base_url":   "http://itam.local",
	})
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "%PDF") {
		t.Fatalf("labels by tags status=%d", rec.Code)
	}

	// 不指定范围 → 400
	rec = doStocktakeJSON(t, r, http.MethodPost, "/api/v1/stocktakes/labels", token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("labels without scope status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 不存在的任务 → 404（不盲打）
	rec = doStocktakeJSON(t, r, http.MethodPost, "/api/v1/stocktakes/labels", token, gin.H{
		"company_id":   companyID,
		"stocktake_id": 999999,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("labels unknown task status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStocktakeAdminCancelFlow(t *testing.T) {
	r := setupStocktakeRouter(t)
	companyID, assetIDs, _ := seedStocktakeFixture(t)
	token := adminToken(t)

	// 草稿直接取消
	created := createStocktakeViaAPI(t, r, companyID, "取消任务", assetIDs[:1])
	rec := doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/cancel", created.ID), token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel draft status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 盘点中也可取消；已取消的再取消 → 400
	second := createStocktakeViaAPI(t, r, companyID, "再取消", assetIDs[:1])
	startStocktakeViaAPI(t, r, companyID, second.ID)
	rec = doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/cancel", second.ID), token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel processing status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/cancel", second.ID), token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("double cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStocktakeTokenRotateInvalidatesOld(t *testing.T) {
	r := setupStocktakeRouter(t)
	companyID, assetIDs, _ := seedStocktakeFixture(t)
	token := adminToken(t)

	created := createStocktakeViaAPI(t, r, companyID, "令牌轮换", assetIDs[:1])
	old := startStocktakeViaAPI(t, r, companyID, created.ID)

	rec := doStocktakeJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/v1/stocktakes/%d/rotate-token", created.ID), token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doStocktakeJSON(t, r, http.MethodGet, "/api/v1/public/stocktakes/"+old, "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("old token after rotate status=%d body=%s", rec.Code, rec.Body.String())
	}
}
