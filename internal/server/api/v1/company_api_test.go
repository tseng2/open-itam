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
// 口径）、更新改名、删除活跃引用拦截 409 带明细（软删行不阻塞——
// 否则「删资产再删公司」被死锁）、零引用可删（硬删不占名）、
// 写面 admin 收口读面登录可读

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

	// 软删资产（API 删除的真实语义）后，软删行不得阻塞公司删除——
	// 否则「删资产再删公司」的正常流程被死锁且无 UI 途径清理
	if err := store.DB.Delete(&model.Asset{}, asset.ID).Error; err != nil {
		t.Fatalf("soft-delete asset: %v", err)
	}
	rec = doStorageJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/companies/%d", companyID), token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("soft-deleted rows must not block: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var blockedResp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &blockedResp); err != nil {
		t.Fatalf("decode blocked resp: %v", err)
	}
	if !strings.Contains(blockedResp.Message, "移动存储领用 1") || strings.Contains(blockedResp.Message, "资产") {
		t.Fatalf("only active lending should block: %s", blockedResp.Message)
	}

	// 软删领用（活跃引用清零）后可删；软删行成为孤儿残留（可接受，展示层不可见）
	store.DB.Where("company_id = ?", companyID).Delete(&model.StorageLending{})
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

// 公司区域（region）round-trip 契约（2026-09-26 市级漫游升级）：
// 「省|市」创建/更新/读回一致、分段空格归一、空串合法（跳过地理维）、
// 三段以上 400；region 是漫游判定的按公司基准，组织页维护
func TestCompanyRegionRoundTrip(t *testing.T) {
	r := setupCompanyRouter(t)
	token := adminToken(t)

	rec := doStorageJSON(t, r, http.MethodPost, "/api/v1/companies", token, gin.H{
		"name": "区域基准测试公司", "code": "REGION", "region": " 广东省 | 东莞市 ",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create with region status=%d body=%s", rec.Code, rec.Body.String())
	}
	regionID := companyIDOf(t, rec)
	var resp struct {
		Data model.Company `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Region != "广东省|东莞市" {
		t.Fatalf("region must normalize to 广东省|东莞市, got %q", resp.Data.Region)
	}

	// 更新改区域（苏州分公司口径）→ 读回一致；清空（空串）→ 跳过地理维合法
	rec = doStorageJSON(t, r, http.MethodPut, fmt.Sprintf("/api/v1/companies/%d", regionID), token, gin.H{
		"name": "区域基准测试公司", "code": "REGION", "region": "江苏省|苏州市",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update region status=%d body=%s", rec.Code, rec.Body.String())
	}
	resp.Data = model.Company{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if resp.Data.Region != "江苏省|苏州市" {
		t.Fatalf("updated region mismatch: %q", resp.Data.Region)
	}
	rec = doStorageJSON(t, r, http.MethodPut, fmt.Sprintf("/api/v1/companies/%d", regionID), token, gin.H{
		"name": "区域基准测试公司", "code": "REGION", "region": "",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("clear region status=%d body=%s", rec.Code, rec.Body.String())
	}
	resp.Data = model.Company{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cleared: %v", err)
	}
	if resp.Data.Region != "" {
		t.Fatalf("cleared region must be empty, got %q", resp.Data.Region)
	}

	// 三段以上 400（市级比对只认省|市两层）
	rec = doStorageJSON(t, r, http.MethodPut, fmt.Sprintf("/api/v1/companies/%d", regionID), token, gin.H{
		"name": "区域基准测试公司", "code": "REGION", "region": "广东省|东莞市|南城街道",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("three-segment region must 400, status=%d body=%s", rec.Code, rec.Body.String())
	}

	_ = doStorageJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/companies/%d", regionID), token, nil)
}
