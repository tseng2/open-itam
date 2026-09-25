package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// P2 耗材管理：gin 集成测试。
// 覆盖：CRUD 与角色守卫（读面登录可读/写面 admin）、建账库存恒 0、
// 出入库调整端点（API 层把正数量换算为带符号增量）、库存不足 409、
// 有流水删除拦截 409 带计数、预警过滤与 low_stock 派生、流水台账

func setupConsumableRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/consumable_api_test.db")
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
	RegisterConsumableRoutes(protected)
	return r
}

func seedConsumableFixture(t *testing.T) (companyID, adminID, userID int64) {
	t.Helper()
	company := model.Company{Name: "耗材测试公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	admin := model.User{CompanyID: company.ID, Username: "itadmin", RealName: "管理员", Role: "admin", Status: "active"}
	user := model.User{CompanyID: company.ID, Username: "zhangsan", RealName: "张三", Role: "user", Status: "active"}
	for _, u := range []*model.User{&admin, &user} {
		if err := store.DB.Create(u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	return company.ID, admin.ID, user.ID
}

func createConsumableViaAPI(t *testing.T, r http.Handler, token string, body gin.H) model.Consumable {
	t.Helper()
	rec := doDispatchJSON(t, r, http.MethodPost, "/api/v1/consumables", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create consumable: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.Consumable `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Data
}

func listConsumablesViaAPI(t *testing.T, r http.Handler, token, query string) []model.Consumable {
	t.Helper()
	rec := doDispatchJSON(t, r, http.MethodGet, "/api/v1/consumables"+query, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list consumables: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Items []model.Consumable `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Data.Items
}

func TestConsumableCRUDAndRoleGuard(t *testing.T) {
	r := setupConsumableRouter(t)
	companyID, adminID, userID := seedConsumableFixture(t)
	admin := userTokenFor(t, adminID, "itadmin", "admin")
	user := userTokenFor(t, userID, "zhangsan", "user")

	// 写面仅 admin：user 一律 403
	for _, spec := range []struct {
		method, path string
		body         gin.H
	}{
		{http.MethodPost, "/api/v1/consumables", gin.H{"company_id": companyID, "name": "x"}},
		{http.MethodPost, "/api/v1/consumables/1/stock-in", gin.H{"company_id": companyID, "quantity": 1}},
		{http.MethodPost, "/api/v1/consumables/1/stock-out", gin.H{"company_id": companyID, "quantity": 1}},
		{http.MethodPost, "/api/v1/consumables/1/adjust", gin.H{"company_id": companyID, "delta": -1}},
		{http.MethodPut, "/api/v1/consumables/1", gin.H{"company_id": companyID, "name": "x"}},
		{http.MethodDelete, "/api/v1/consumables/1?company_id=1", nil},
	} {
		rec := doDispatchJSON(t, r, spec.method, spec.path, user, spec.body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("user %s %s must 403, got %d", spec.method, spec.path, rec.Code)
		}
	}

	// 建账：库存恒 0（传入 stock 也强制归零——期初库存走入库流水）
	created := createConsumableViaAPI(t, r, admin, gin.H{
		"company_id": companyID, "name": "A4 打印纸", "spec": "80g/500张/包", "unit": "包",
		"min_quantity": 10, "stock": 999,
	})
	if created.ID == 0 || created.Stock != 0 || created.MinQuantity != 10 {
		t.Fatalf("created mismatch: %+v", created)
	}
	// 重名 409
	rec := doDispatchJSON(t, r, http.MethodPost, "/api/v1/consumables", admin, gin.H{
		"company_id": companyID, "name": "A4 打印纸",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate must 409, got %d", rec.Code)
	}

	// 编辑：只动元数据；库存列不接受直接修改
	rec = doDispatchJSON(t, r, http.MethodPut, fmt.Sprintf("/api/v1/consumables/%d", created.ID), admin, gin.H{
		"company_id": companyID, "name": "A4 打印纸", "spec": "70g/500张/包", "unit": "包",
		"min_quantity": 20, "stock": 555,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var updated struct {
		Data model.Consumable `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Data.Stock != 0 || updated.Data.MinQuantity != 20 {
		t.Fatalf("update must ignore stock and set min_quantity: %+v", updated.Data)
	}

	// 读面登录可读 + low_stock 派生：库存 0 ≤ 预警线 20 → 预警成立
	items := listConsumablesViaAPI(t, r, user, fmt.Sprintf("?company_id=%d", companyID))
	if len(items) != 1 || !items[0].LowStock {
		t.Fatalf("list: %+v", items)
	}

	// 删除：无流水可删；非法请求 400
	rec = doDispatchJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/consumables/%d?company_id=%d", created.ID, companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete no-txn consumable: http=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doDispatchJSON(t, r, http.MethodPost, "/api/v1/consumables/999/stock-in", admin, gin.H{
		"company_id": companyID, "quantity": 1,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stock-in missing consumable must 404, got %d", rec.Code)
	}
}

func TestConsumableStockEndpoints(t *testing.T) {
	r := setupConsumableRouter(t)
	companyID, adminID, _ := seedConsumableFixture(t)
	admin := userTokenFor(t, adminID, "itadmin", "admin")

	c := createConsumableViaAPI(t, r, admin, gin.H{
		"company_id": companyID, "name": "无线鼠标", "unit": "个", "min_quantity": 5,
	})

	// 入库 +50
	rec := doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d/stock-in", c.ID), admin,
		gin.H{"company_id": companyID, "quantity": 50, "remark": "采购入库"})
	if rec.Code != http.StatusOK {
		t.Fatalf("stock-in: http=%d body=%s", rec.Code, rec.Body.String())
	}
	// 非法数量 400
	rec = doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d/stock-in", c.ID), admin,
		gin.H{"company_id": companyID, "quantity": 0})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zero quantity must 400, got %d", rec.Code)
	}

	// 出库 3（正数量输入，落库负增量），带领用人
	rec = doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d/stock-out", c.ID), admin,
		gin.H{"company_id": companyID, "quantity": 3, "recipient": "张三"})
	if rec.Code != http.StatusOK {
		t.Fatalf("stock-out: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Data struct {
			Consumable model.Consumable  `json:"consumable"`
			Txn        model.ConsumableTxn `json:"txn"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Data.Consumable.Stock != 47 || out.Data.Txn.Delta != -3 || out.Data.Txn.Recipient != "张三" {
		t.Fatalf("stock-out payload: %+v", out.Data)
	}
	if out.Data.Txn.OperatorID != adminID || out.Data.Txn.OperatorName == "" {
		t.Fatalf("operator snapshot: %+v", out.Data.Txn)
	}

	// 出库击穿库存 → 409
	rec = doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d/stock-out", c.ID), admin,
		gin.H{"company_id": companyID, "quantity": 100})
	if rec.Code != http.StatusConflict {
		t.Fatalf("insufficient must 409, got %d", rec.Code)
	}

	// 调整盘亏 -2；零调整 400
	rec = doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d/adjust", c.ID), admin,
		gin.H{"company_id": companyID, "delta": -2, "remark": "盘点盘亏"})
	if rec.Code != http.StatusOK {
		t.Fatalf("adjust: http=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d/adjust", c.ID), admin,
		gin.H{"company_id": companyID, "delta": 0})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zero adjust must 400, got %d", rec.Code)
	}

	// 列表：库存 45 ≤ 预警线 5？不——45 > 5，不预警；low_stock=false
	items := listConsumablesViaAPI(t, r, admin, fmt.Sprintf("?company_id=%d", companyID))
	if len(items) != 1 || items[0].Stock != 45 || items[0].LowStock {
		t.Fatalf("after ledger: %+v", items)
	}
	// 出库 42 → 库存 3 ≤ 5 触发预警
	rec = doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d/stock-out", c.ID), admin,
		gin.H{"company_id": companyID, "quantity": 42})
	if rec.Code != http.StatusOK {
		t.Fatalf("drain stock: http=%d body=%s", rec.Code, rec.Body.String())
	}
	items = listConsumablesViaAPI(t, r, admin, fmt.Sprintf("?company_id=%d&low_stock=true", companyID))
	if len(items) != 1 || !items[0].LowStock || items[0].Stock != 3 {
		t.Fatalf("low-stock view: %+v", items)
	}

	// 流水台账：全部 4 条；出库过滤 2 条
	rec = doDispatchJSON(t, r, http.MethodGet, fmt.Sprintf("/api/v1/consumables/%d/txns?company_id=%d", c.ID, companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("txns: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var ledger struct {
		Data struct {
			Total int64                   `json:"total"`
			Items []model.ConsumableTxn   `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &ledger)
	if ledger.Data.Total != 4 || len(ledger.Data.Items) != 4 {
		t.Fatalf("ledger total: %+v", ledger.Data)
	}
	rec = doDispatchJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/consumables/%d/txns?company_id=%d&type=stock_out", c.ID, companyID), admin, nil)
	json.Unmarshal(rec.Body.Bytes(), &ledger)
	if ledger.Data.Total != 2 {
		t.Fatalf("stock_out ledger: %+v", ledger.Data)
	}

	// 有流水的耗材删除 → 409 带计数
	rec = doDispatchJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/consumables/%d?company_id=%d", c.ID, companyID), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete with txns must 409, got %d", rec.Code)
	}
	var conflict struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(rec.Body.Bytes(), &conflict)
	if conflict.Code != 40902 {
		t.Fatalf("delete conflict envelope: %+v", conflict)
	}
}
