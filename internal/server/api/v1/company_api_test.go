package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// 公司 CRUD 契约测试：名称全局唯一 409（Unscoped 与 DB 唯一索引同
// 口径）、更新改名、删除全业务表引用拦截 409 带明细、零引用可删
//（硬删不占名）、写面 admin 收口读面登录可读

func setupCompanyRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := store.InitDB("sqlite", t.TempDir()+"/company_test.db")
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
	RegisterCompanyRoutes(protected)
	return r
}

func companyIDOf(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	var resp struct {
		Code int `json:"code"`
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("resp decode: err=%v code=%d body=%s", err, resp.Code, rec.Body.String())
	}
	return resp.Data.ID
}

func TestCompanyCRUDAndUniqueness(t *testing.T) {
	r := setupCompanyRouter(t)
	token := adminToken(t)

	rec := doStorageJSON(t, r, http.MethodPost, "/api/v1/companies", token, gin.H{
		"name": "公司CRUD测试甲", "code": "COA", "domain": "a.example.com",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	createdID := companyIDOf(t, rec)

	// 重名 409
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/companies", token, gin.H{
		"name": "公司CRUD测试甲", "code": "COA2",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate name status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 改名 + 改编码
	rec = doStorageJSON(t, r, http.MethodPut, fmt.Sprintf("/api/v1/companies/%d", createdID), token, gin.H{
		"name": "公司CRUD测试甲改", "code": "COA-NEW", "domain": "",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.Company `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if resp.Data.Name != "公司CRUD测试甲改" || resp.Data.Code != "COA-NEW" {
		t.Fatalf("update wrong: %+v", resp.Data)
	}

	// 建第二家 → 改成与第一家同名 → 409
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/companies", token, gin.H{
		"name": "公司CRUD测试乙", "code": "COB",
	})
	secondID := companyIDOf(t, rec)
	rec = doStorageJSON(t, r, http.MethodPut, fmt.Sprintf("/api/v1/companies/%d", secondID), token, gin.H{
		"name": "公司CRUD测试甲改", "code": "COB",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("rename conflict status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 零引用可删（硬删）；删后同名可重建
	rec = doStorageJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/companies/%d", secondID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete clean status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/companies", token, gin.H{
		"name": "公司CRUD测试乙", "code": "COB",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("recreate after hard-delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	recreatedID := companyIDOf(t, rec)

	// 收尾清理（零引用）
	_ = doStorageJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/companies/%d", createdID), token, nil)
	_ = doStorageJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/companies/%d", recreatedID), token, nil)
}

func TestCompanyDeleteBlockedByReferences(t *testing.T) {
	r := setupCompanyRouter(t)
	token := adminToken(t)

	rec := doStorageJSON(t, r, http.MethodPost, "/api/v1/companies", token, gin.H{
		"name": "引用拦截测试公司", "code": "REF",
	})
	companyID := companyIDOf(t, rec)

	// 挂一台资产 + 一条移动存储领用 → 删除被拦
	asset := model.Asset{CompanyID: companyID, CategoryID: 1, AssetTag: "AST-CO-REF-1", Status: 10}
	if err := store.DB.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	lending := model.StorageLending{CompanyID: companyID, Borrower: "李四"}
	if err := store.DB.Create(&lending).Error; err != nil {
		t.Fatalf("seed lending: %v", err)
	}
	rec = doStorageJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/companies/%d", companyID), token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("blocked delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode blocked resp: %v", err)
	}
	if !strings.Contains(resp.Message, "资产 1") || !strings.Contains(resp.Message, "移动存储领用 1") {
		t.Fatalf("blocked message must name ref counts: %s", resp.Message)
	}

	// 清空引用后可删
	store.DB.Unscoped().Where("company_id = ?", companyID).Delete(&model.Asset{})
	store.DB.Unscoped().Where("company_id = ?", companyID).Delete(&model.StorageLending{})
	rec = doStorageJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/companies/%d", companyID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("clean delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// 写面收口 admin：user 增改删 403，读面保持登录可读
func TestCompanyWriteRequiresAdminRole(t *testing.T) {
	r := setupCompanyRouter(t)
	token, err := middleware.GenerateToken(2, "ordinary", "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	rec := doStorageJSON(t, r, http.MethodGet, "/api/v1/companies", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user read status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/companies", token, gin.H{"name": "x", "code": "x"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user create status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStorageJSON(t, r, http.MethodPut, "/api/v1/companies/1", token, gin.H{"name": "x"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user update status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStorageJSON(t, r, http.MethodDelete, "/api/v1/companies/1", token, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}
