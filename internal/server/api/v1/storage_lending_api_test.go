package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// 移动存储领用 API 契约测试：PUT 指针字段语义是
// 未传=保持原值、显式 null=清空（前端编辑清日期必须带 null）——
// 指针反序列化无法区分两者，靠原始键集合判定，此处锁死契约防回退

type storageLendingResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type storageLendingPage struct {
	Total int64                   `json:"total"`
	Items []model.StorageLending `json:"items"`
}

func setupStorageLendingRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/storage_lending_test.db")
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
	RegisterStorageLendingRoutes(protected)
	return r
}

func seedStorageLendingCompany(t *testing.T) int64 {
	t.Helper()
	company := model.Company{Name: "移动存储测试公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	return company.ID
}

func doStorageJSON(t *testing.T, r http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw := []byte(nil)
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		raw = b
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decodeStorageLending(t *testing.T, rec *httptest.ResponseRecorder) model.StorageLending {
	t.Helper()
	var resp storageLendingResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("resp decode: err=%v code=%d body=%s", err, resp.Code, rec.Body.String())
	}
	var item model.StorageLending
	if err := json.Unmarshal(resp.Data, &item); err != nil {
		t.Fatalf("decode storage lending data: %v", err)
	}
	return item
}

func listStorageLendings(t *testing.T, r http.Handler, token, query string) storageLendingPage {
	t.Helper()
	rec := doStorageJSON(t, r, http.MethodGet, "/api/v1/storage-lendings"+query, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp storageLendingResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("list resp decode: err=%v code=%d", err, resp.Code)
	}
	var page storageLendingPage
	if err := json.Unmarshal(resp.Data, &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	return page
}

func TestStorageLendingCreateListUpdateFlow(t *testing.T) {
	r := setupStorageLendingRouter(t)
	companyID := seedStorageLendingCompany(t)
	token := adminToken(t)

	// 建账：borrower 必填、quantity 缺省 1
	rec := doStorageJSON(t, r, http.MethodPost, "/api/v1/storage-lendings", token, gin.H{
		"company_id":    companyID,
		"department":    "研发部",
		"borrower":      "李四",
		"borrow_date":   "2026-01-05T00:00:00Z",
		"brand":         "金士顿",
		"spec":          "128G",
		"device_code":   "USB-0001",
		"sec_certified": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	created := decodeStorageLending(t, rec)
	if created.Borrower != "李四" || created.Quantity != 1 || !created.SecCertified || created.BorrowDate == nil {
		t.Fatalf("unexpected created record: %+v", created)
	}

	// borrower 缺失 → 400
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/storage-lendings", token, gin.H{"company_id": companyID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing borrower status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 同公司第二条：quantity 显式 3
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/storage-lendings", token, gin.H{
		"company_id":  companyID,
		"department": "财务部",
		"borrower":   "王五",
		"quantity":   3,
	})
	second := decodeStorageLending(t, rec)
	if second.Quantity != 3 {
		t.Fatalf("expected quantity 3, got %d", second.Quantity)
	}

	// 列表：department/borrower 模糊过滤 + company_id 边界
	if page := listStorageLendings(t, r, token, fmt.Sprintf("?company_id=%d&department=研发", companyID)); page.Total != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("department fuzzy filter: total=%d items=%+v", page.Total, page.Items)
	}
	if page := listStorageLendings(t, r, token, fmt.Sprintf("?company_id=%d&borrower=王", companyID)); page.Total != 1 || page.Items[0].ID != second.ID {
		t.Fatalf("borrower fuzzy filter: total=%d", page.Total)
	}
	if page := listStorageLendings(t, r, token, "?company_id=999999"); page.Total != 0 {
		t.Fatalf("company boundary filter: total=%d", page.Total)
	}

	// 未传保持：只改 brand/spec，其余字段（borrower/borrow_date）不动
	rec = doStorageJSON(t, r, http.MethodPut,
		fmt.Sprintf("/api/v1/storage-lendings/%d", created.ID), token, gin.H{"brand": "闪迪", "spec": "256G"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := decodeStorageLending(t, rec)
	if updated.Brand != "闪迪" || updated.Spec != "256G" || updated.Borrower != "李四" || updated.BorrowDate == nil {
		t.Fatalf("partial update must keep unspecified fields: %+v", updated)
	}

	// 显式 null 清空：前端编辑清日期必须带 null
	rec = doStorageJSON(t, r, http.MethodPut,
		fmt.Sprintf("/api/v1/storage-lendings/%d", created.ID), token, gin.H{"borrow_date": nil})
	if rec.Code != http.StatusOK {
		t.Fatalf("null-clear update status=%d body=%s", rec.Code, rec.Body.String())
	}
	if cleared := decodeStorageLending(t, rec); cleared.BorrowDate != nil {
		t.Fatalf("explicit null must clear borrow_date, got %v", cleared.BorrowDate)
	}

	// 字符串字段显式 null 同语义清空
	rec = doStorageJSON(t, r, http.MethodPut,
		fmt.Sprintf("/api/v1/storage-lendings/%d", created.ID), token, gin.H{"department": nil})
	if cleared := decodeStorageLending(t, rec); cleared.Department != "" {
		t.Fatalf("explicit null must clear department, got %q", cleared.Department)
	}

	// 归还回填：PUT return_date/return_qty
	rec = doStorageJSON(t, r, http.MethodPut,
		fmt.Sprintf("/api/v1/storage-lendings/%d", second.ID), token,
		gin.H{"return_date": "2026-03-01T00:00:00Z", "return_qty": 2})
	returned := decodeStorageLending(t, rec)
	if returned.ReturnDate == nil || returned.ReturnQty != 2 {
		t.Fatalf("return backfill: %+v", returned)
	}
	if returned.Quantity != 3 || returned.Borrower != "王五" {
		t.Fatalf("return backfill must not touch other fields: %+v", returned)
	}
}

func TestStorageLendingUpdateBadIDAndNotFound(t *testing.T) {
	r := setupStorageLendingRouter(t)
	seedStorageLendingCompany(t)
	token := adminToken(t)

	rec := doStorageJSON(t, r, http.MethodPut, "/api/v1/storage-lendings/abc", token, gin.H{"brand": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStorageJSON(t, r, http.MethodPut, "/api/v1/storage-lendings/999999", token, gin.H{"brand": "x"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found status=%d body=%s", rec.Code, rec.Body.String())
	}
}
