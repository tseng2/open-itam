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

// P2 软件许可管理：gin 集成测试。
// 覆盖：CRUD 与角色守卫（读面登录可读/写面 admin）、状态日期派生、
// 已用席位富化、资产挂接席位与超用拦截（409）、解除挂接、
// 挂接中删除拦截（409 带计数）、到期窗口过滤、跨公司挂接 404

func setupLicenseRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/license_api_test.db")
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
	RegisterLicenseRoutes(protected)
	// 席位挂接走真实台账路由面（建账/编辑带 license_id）
	RegisterAssetRoutes(protected, 15*time.Minute)
	return r
}

func seedLicenseFixture(t *testing.T) (companyID, otherCompanyID, adminID, userID int64) {
	t.Helper()
	company := model.Company{Name: "许可测试公司-" + t.Name()}
	other := model.Company{Name: "许可外公司-" + t.Name()}
	for _, c := range []*model.Company{&company, &other} {
		if err := store.DB.Create(c).Error; err != nil {
			t.Fatalf("seed company: %v", err)
		}
	}
	admin := model.User{CompanyID: company.ID, Username: "itadmin", RealName: "管理员", Role: "admin", Status: "active"}
	user := model.User{CompanyID: company.ID, Username: "zhangsan", RealName: "张三", Role: "user", Status: "active"}
	for _, u := range []*model.User{&admin, &user} {
		if err := store.DB.Create(u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	return company.ID, other.ID, admin.ID, user.ID
}

func createLicenseViaAPI(t *testing.T, r http.Handler, token string, body gin.H) model.License {
	t.Helper()
	rec := doDispatchJSON(t, r, http.MethodPost, "/api/v1/licenses", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create license: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.License `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Data
}

func listLicensesViaAPI(t *testing.T, r http.Handler, token, query string) []model.License {
	t.Helper()
	rec := doDispatchJSON(t, r, http.MethodGet, "/api/v1/licenses"+query, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list licenses: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Items []model.License `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Data.Items
}

func createAssetWithLicense(t *testing.T, r http.Handler, token string, companyID int64, tag string, licenseID *int64) *httptest.ResponseRecorder {
	t.Helper()
	body := gin.H{
		"company_id":  companyID,
		"category_id": 2, "category_name": "笔记本电脑",
		"asset_tag": tag,
		"status":     model.AssetStatusStock,
	}
	if licenseID != nil {
		body["license_id"] = *licenseID
	}
	return doDispatchJSON(t, r, http.MethodPost, "/api/v1/assets", token, body)
}

func TestLicenseCRUDAndRoleGuard(t *testing.T) {
	r := setupLicenseRouter(t)
	companyID, otherCompanyID, adminID, userID := seedLicenseFixture(t)
	admin := userTokenFor(t, adminID, "itadmin", "admin")
	user := userTokenFor(t, userID, "zhangsan", "user")

	// 写面仅 admin：user 建/改/删一律 403
	rec := doDispatchJSON(t, r, http.MethodPost, "/api/v1/licenses", user, gin.H{
		"company_id": companyID, "name": "Office", "total_seats": 10,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user create must 403, got %d", rec.Code)
	}
	rec = doDispatchJSON(t, r, http.MethodDelete, "/api/v1/licenses/1?company_id=1", user, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user delete must 403, got %d", rec.Code)
	}

	// admin 创建：状态由日期派生（永久授权 = 在用）
	in30 := time.Now().AddDate(0, 0, 30).UTC().Format(time.RFC3339)
	past := time.Now().AddDate(0, 0, -3).UTC().Format(time.RFC3339)
	permanent := createLicenseViaAPI(t, r, admin, gin.H{
		"company_id": companyID, "name": "Foxit PDF 编辑器", "vendor": "福昕软件",
		"category": "文档工具", "total_seats": 0,
	})
	if permanent.Status != model.LicenseStatusActive || permanent.UsedSeats != 0 {
		t.Fatalf("permanent license: %+v", permanent)
	}
	expiring := createLicenseViaAPI(t, r, admin, gin.H{
		"company_id": companyID, "name": "AutoCAD 2024", "vendor": "Autodesk",
		"total_seats": 2, "expiration_date": in30,
	})
	expired := createLicenseViaAPI(t, r, admin, gin.H{
		"company_id": companyID, "name": "旧版 Photoshop", "total_seats": 1,
		"expiration_date": past,
	})
	terminated := createLicenseViaAPI(t, r, admin, gin.H{
		"company_id": companyID, "name": "已退订 SaaS", "total_seats": 1,
		"termination_date": past,
	})
	if expiring.Status != model.LicenseStatusActive {
		t.Fatalf("expiring license must be active: %+v", expiring)
	}
	if expired.Status != model.LicenseStatusExpired {
		t.Fatalf("expired license must be expired: %+v", expired)
	}
	if terminated.Status != model.LicenseStatusTerminated {
		t.Fatalf("terminated license must be terminated: %+v", terminated)
	}

	// 读面登录可读 + 公司边界 + 状态过滤在响应中派生
	items := listLicensesViaAPI(t, r, user, fmt.Sprintf("?company_id=%d", companyID))
	if len(items) != 4 {
		t.Fatalf("company must see 4 licenses, got %d", len(items))
	}
	if len(listLicensesViaAPI(t, r, user, fmt.Sprintf("?company_id=%d", otherCompanyID))) != 0 {
		t.Fatal("cross-company list must be empty")
	}
	// 到期窗口：30 天内到期且未终止 → 只有 AutoCAD
	inWindow := listLicensesViaAPI(t, r, admin, fmt.Sprintf("?company_id=%d&expiring_days=45", companyID))
	if len(inWindow) != 1 || inWindow[0].ID != expiring.ID {
		t.Fatalf("expiring window: %+v", inWindow)
	}

	// 更新：改席位；跨公司 404
	rec = doDispatchJSON(t, r, http.MethodPut, fmt.Sprintf("/api/v1/licenses/%d", expiring.ID), admin, gin.H{
		"company_id": companyID, "name": "AutoCAD 2024", "vendor": "Autodesk",
		"total_seats": 5, "expiration_date": in30,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update license: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var updated struct {
		Data model.License `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Data.TotalSeats != 5 {
		t.Fatalf("updated seats: %+v", updated.Data)
	}
	rec = doDispatchJSON(t, r, http.MethodPut, fmt.Sprintf("/api/v1/licenses/%d", expiring.ID), admin, gin.H{
		"company_id": otherCompanyID, "name": "x",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company update must 404, got %d", rec.Code)
	}

	// 详情：正常返回带派生状态与席位计数；跨公司 404
	rec = doDispatchJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/licenses/%d?company_id=%d", expiring.ID, companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get license: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Data model.License `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &detail)
	if detail.Data.Status != model.LicenseStatusActive || detail.Data.UsedSeats != 0 {
		t.Fatalf("license detail enrichment: %+v", detail.Data)
	}
	rec = doDispatchJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/licenses/%d?company_id=%d", expiring.ID, otherCompanyID), admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company get must 404, got %d", rec.Code)
	}

	// 删除：user 403（已验）；admin 删除未挂接的过期许可成功
	rec = doDispatchJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/licenses/%d?company_id=%d", expired.ID, companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete license: http=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLicenseSeatBindingAndCap(t *testing.T) {
	r := setupLicenseRouter(t)
	companyID, otherCompanyID, adminID, userID := seedLicenseFixture(t)
	_ = userID
	admin := userTokenFor(t, adminID, "itadmin", "admin")

	license := createLicenseViaAPI(t, r, admin, gin.H{
		"company_id": companyID, "name": "IntelliJ IDEA", "vendor": "JetBrains",
		"total_seats": 2,
	})
	otherLicense := createLicenseViaAPI(t, r, admin, gin.H{
		"company_id": otherCompanyID, "name": "别家的许可", "total_seats": 9,
	})
	licenseID := license.ID

	// 跨公司挂接 → 404
	rec := createAssetWithLicense(t, r, admin, companyID, "AST-LIC-X1", &otherLicense.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company license binding must 404, got %d", rec.Code)
	}

	// 两席占满
	for _, tag := range []string{"AST-LIC-A", "AST-LIC-B"} {
		rec = createAssetWithLicense(t, r, admin, companyID, tag, &licenseID)
		if rec.Code != http.StatusOK {
			t.Fatalf("bind %s: http=%d body=%s", tag, rec.Code, rec.Body.String())
		}
	}
	items := listLicensesViaAPI(t, r, admin, fmt.Sprintf("?company_id=%d", companyID))
	var bound *model.License
	for i := range items {
		if items[i].ID == licenseID {
			bound = &items[i]
		}
	}
	if bound == nil || bound.UsedSeats != 2 {
		t.Fatalf("used seats must be 2, got %+v", bound)
	}

	// 第三台资产挂接 → 席位已满 409
	rec = createAssetWithLicense(t, r, admin, companyID, "AST-LIC-C", &licenseID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("seat cap must 409, got %d body=%s", rec.Code, rec.Body.String())
	}

	// 解除 A 的挂接（编辑 license_id=0）→ 席位释放，C 可挂
	rec = doDispatchJSON(t, r, http.MethodPut, "/api/v1/assets/1", admin, gin.H{
		"company_id": companyID, "license_id": 0,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("unbind license: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var unbound struct {
		Data model.Asset `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &unbound)
	if unbound.Data.LicenseID != nil || unbound.Data.LicenseName != "" {
		t.Fatalf("asset must be unbound, got %+v", unbound.Data)
	}
	rec = createAssetWithLicense(t, r, admin, companyID, "AST-LIC-C", &licenseID)
	if rec.Code != http.StatusOK {
		t.Fatalf("rebind after release: http=%d body=%s", rec.Code, rec.Body.String())
	}

	// 建账响应与详情的许可富化（LicenseName）
	var created struct {
		Data model.Asset `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Data.LicenseName != "IntelliJ IDEA" {
		t.Fatalf("asset license enrichment: %+v", created.Data)
	}
	rec = doDispatchJSON(t, r, http.MethodGet, "/api/v1/assets/2", admin, nil)
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Data.LicenseName != "IntelliJ IDEA" {
		t.Fatalf("asset detail enrichment: %+v", created.Data)
	}

	// 挂接中的许可删除 → 409 带计数；全部解绑后可删
	rec = doDispatchJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/licenses/%d?company_id=%d", licenseID, companyID), admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete bound license must 409, got %d body=%s", rec.Code, rec.Body.String())
	}
	var conflictResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(rec.Body.Bytes(), &conflictResp)
	if conflictResp.Code != 40902 || conflictResp.Message == "" {
		t.Fatalf("delete conflict envelope: %+v", conflictResp)
	}
}
