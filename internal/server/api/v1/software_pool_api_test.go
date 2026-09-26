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

// 阶段三软件合规 API 契约测试：受控池 CRUD（读面登录可读 + 写面 admin、
// 名称同公司唯一 409、许可挂接校验 404/0 解除）+ 合规报表（admin-only、
// 超用判定与未受控清单分页）+ 许可删除被池项挂接拦截 409

func setupSoftwarePoolRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/software_pool_test.db")
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
	RegisterSoftwarePoolRoutes(protected)
	RegisterSoftwareComplianceRoutes(protected)
	RegisterLicenseRoutes(protected)
	return r
}

type softwarePoolResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type softwarePoolPage struct {
	Total int64                 `json:"total"`
	Items []model.SoftwarePool `json:"items"`
}

func seedSoftwarePoolFixture(t *testing.T) (companyID, otherCompanyID, licenseID int64) {
	t.Helper()
	ca := model.Company{Name: "软件合规测试公司", Code: "SWPA"}
	cb := model.Company{Name: "软件合规对照公司", Code: "SWPB"}
	for _, c := range []*model.Company{&ca, &cb} {
		if err := store.DB.Create(c).Error; err != nil {
			t.Fatalf("seed company: %v", err)
		}
	}
	lic := model.License{CompanyID: ca.ID, Name: "M365 商业版", TotalSeats: 2}
	if err := store.DB.Create(&lic).Error; err != nil {
		t.Fatalf("seed license: %v", err)
	}
	return ca.ID, cb.ID, lic.ID
}

func decodeSoftwarePool(t *testing.T, rec *httptest.ResponseRecorder) model.SoftwarePool {
	t.Helper()
	var resp softwarePoolResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("resp decode: err=%v code=%d body=%s", err, resp.Code, rec.Body.String())
	}
	var item model.SoftwarePool
	if err := json.Unmarshal(resp.Data, &item); err != nil {
		t.Fatalf("decode pool data: %v", err)
	}
	return item
}

func TestSoftwarePoolCRUDFlow(t *testing.T) {
	r := setupSoftwarePoolRouter(t)
	companyID, otherCompanyID, licenseID := seedSoftwarePoolFixture(t)
	token := adminToken(t)

	// 建池：挂接许可 + 富化回带许可名
	rec := doStorageJSON(t, r, http.MethodPost, "/api/v1/software-pools", token, gin.H{
		"company_id": companyID, "name": "Microsoft 365", "vendor": "Microsoft",
		"category": "办公套件", "license_id": licenseID, "remark": "含桌面端",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	created := decodeSoftwarePool(t, rec)
	if created.Name != "Microsoft 365" || created.LicenseID == nil || *created.LicenseID != licenseID {
		t.Fatalf("unexpected created pool: %+v", created)
	}
	if created.LicenseName != "M365 商业版" {
		t.Fatalf("license name enrichment missing: %+v", created)
	}

	// 同名 409（名称同公司唯一，软删不占名）
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/software-pools", token, gin.H{
		"company_id": companyID, "name": "Microsoft 365",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate name status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 跨公司许可挂接 → 404
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/software-pools", token, gin.H{
		"company_id": otherCompanyID, "name": "别的软件", "license_id": licenseID,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-company license status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 编辑：改名 + 解除许可挂接（license_id 0 → NULL）
	rec = doStorageJSON(t, r, http.MethodPut,
		fmt.Sprintf("/api/v1/software-pools/%d", created.ID), token, gin.H{
			"company_id": companyID, "name": "Microsoft 365 商业版", "license_id": 0,
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := decodeSoftwarePool(t, rec)
	if updated.Name != "Microsoft 365 商业版" || updated.LicenseID != nil {
		t.Fatalf("update must rename and unlink: %+v", updated)
	}

	// 重新挂回许可（后续合规报表与删除拦截用例依赖）
	rec = doStorageJSON(t, r, http.MethodPut,
		fmt.Sprintf("/api/v1/software-pools/%d", created.ID), token, gin.H{
			"company_id": companyID, "name": "Microsoft 365 商业版", "license_id": licenseID,
		})
	linked := decodeSoftwarePool(t, rec)
	if linked.LicenseID == nil || *linked.LicenseID != licenseID {
		t.Fatalf("relink failed: %+v", linked)
	}

	// 列表：keyword 模糊 + 富化
	rec = doStorageJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/software-pools?company_id=%d&keyword=365", companyID), token, nil)
	var resp softwarePoolResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("list resp decode: err=%v code=%d", err, resp.Code)
	}
	var page softwarePoolPage
	if err := json.Unmarshal(resp.Data, &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("list filter wrong: total=%d items=%+v", page.Total, page.Items)
	}
	if page.Items[0].LicenseName != "M365 商业版" {
		t.Fatalf("list enrichment missing: %+v", page.Items[0])
	}

	// 删除
	rec = doStorageJSON(t, r, http.MethodDelete,
		fmt.Sprintf("/api/v1/software-pools/%d?company_id=%d", created.ID, companyID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStorageJSON(t, r, http.MethodDelete,
		fmt.Sprintf("/api/v1/software-pools/%d?company_id=%d", created.ID, companyID), token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("double delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSoftwarePoolWriteRequiresAdminRole(t *testing.T) {
	r := setupSoftwarePoolRouter(t)
	companyID, _, _ := seedSoftwarePoolFixture(t)
	token, err := middleware.GenerateToken(2, "ordinary", "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	rec := doStorageJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/software-pools?company_id=%d", companyID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user read status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/software-pools", token, gin.H{
		"company_id": companyID, "name": "x",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user create status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStorageJSON(t, r, http.MethodPut, "/api/v1/software-pools/1", token, gin.H{
		"company_id": companyID, "name": "x",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user update status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStorageJSON(t, r, http.MethodDelete, "/api/v1/software-pools/1?company_id=1", token, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func seedDeviceSoftware(t *testing.T, companyID int64, devices []string, name string) {
	t.Helper()
	for _, dev := range devices {
		row := model.DeviceSoftware{
			CompanyID: companyID, DeviceID: dev, AssetID: 1,
			Name: name, Version: "1.0", SeenAt: time.Now().UTC(),
		}
		if err := store.DB.Create(&row).Error; err != nil {
			t.Fatalf("seed device software: %v", err)
		}
	}
}

func TestSoftwareComplianceReport(t *testing.T) {
	r := setupSoftwarePoolRouter(t)
	companyID, _, licenseID := seedSoftwarePoolFixture(t)
	token := adminToken(t)

	// 池：M365 挂 2 席许可；WPS 不挂（不限席位）
	pools := []model.SoftwarePool{
		{CompanyID: companyID, Name: "Microsoft 365", LicenseID: &licenseID},
		{CompanyID: companyID, Name: "WPS Office"},
	}
	for i := range pools {
		if err := store.DB.Create(&pools[i]).Error; err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}
	// 安装：M365 4 台（超用）、WPS 1 台（合规）、AutoCAD 2 台（未受控）、
	// VC++ 运行库 9 台（白名单排除）
	seedDeviceSoftware(t, companyID, []string{"d1", "d2", "d3", "d4"}, "Microsoft 365 Apps for enterprise")
	seedDeviceSoftware(t, companyID, []string{"d1"}, "WPS Office 2023")
	seedDeviceSoftware(t, companyID, []string{"d1", "d2"}, "AutoCAD 2026")
	seedDeviceSoftware(t, companyID, []string{"d1", "d2", "d3", "d5", "d6", "d7", "d8", "d9", "d10"},
		"Microsoft Visual C++ 2015-2019 Redistributable (x64)")

	rec := doStorageJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/software-compliance?company_id=%d&page=1&page_size=50", companyID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Summary struct {
				PoolCount       int   `json:"pool_count"`
				OverusedCount   int   `json:"overused_count"`
				ManagedInstalls int64 `json:"managed_installs"`
				UnmanagedCount  int64 `json:"unmanaged_count"`
			} `json:"summary"`
			PoolItems []struct {
				PoolID     int64  `json:"pool_id"`
				Name       string `json:"name"`
				TotalSeats int    `json:"total_seats"`
				Installs   int64  `json:"installs"`
				Overused   bool   `json:"overused"`
			} `json:"pool_items"`
			Unmanaged struct {
				Total int `json:"total"`
				Items []struct {
					Name     string `json:"name"`
					Installs int64  `json:"installs"`
				} `json:"items"`
			} `json:"unmanaged"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("compliance decode: err=%v code=%d body=%s", err, resp.Code, rec.Body.String())
	}
	if resp.Data.Summary.PoolCount != 2 || resp.Data.Summary.OverusedCount != 1 ||
		resp.Data.Summary.ManagedInstalls != 5 || resp.Data.Summary.UnmanagedCount != 1 {
		t.Fatalf("summary wrong: %+v", resp.Data.Summary)
	}
	if len(resp.Data.PoolItems) != 2 {
		t.Fatalf("pool items = %d", len(resp.Data.PoolItems))
	}
	m365 := resp.Data.PoolItems[0]
	if !m365.Overused || m365.Installs != 4 || m365.TotalSeats != 2 {
		t.Fatalf("m365 pool item wrong: %+v", m365)
	}
	if resp.Data.Unmanaged.Total != 1 || len(resp.Data.Unmanaged.Items) != 1 ||
		resp.Data.Unmanaged.Items[0].Name != "AutoCAD 2026" || resp.Data.Unmanaged.Items[0].Installs != 2 {
		t.Fatalf("unmanaged wrong: %+v", resp.Data.Unmanaged)
	}

	// 未受控分页：page_size=1 只回一行
	rec = doStorageJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/software-compliance?company_id=%d&page=1&page_size=1", companyID), token, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("compliance paged decode: %v", err)
	}
	if resp.Data.Unmanaged.Total != 1 || len(resp.Data.Unmanaged.Items) != 1 {
		t.Fatalf("unmanaged pagination wrong: %+v", resp.Data.Unmanaged)
	}
	rec = doStorageJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/software-compliance?company_id=%d&page=2&page_size=1", companyID), token, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("compliance page2 decode: %v", err)
	}
	if len(resp.Data.Unmanaged.Items) != 0 {
		t.Fatalf("page2 must be empty: %+v", resp.Data.Unmanaged)
	}

	// 合规报表 admin-only
	userToken, _ := middleware.GenerateToken(3, "ordinary", "user")
	rec = doStorageJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/software-compliance?company_id=%d", companyID), userToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLicenseDeleteBlockedBySoftwarePoolRef(t *testing.T) {
	r := setupSoftwarePoolRouter(t)
	companyID, _, licenseID := seedSoftwarePoolFixture(t)
	token := adminToken(t)

	pool := model.SoftwarePool{CompanyID: companyID, Name: "Microsoft 365", LicenseID: &licenseID}
	if err := store.DB.Create(&pool).Error; err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	rec := doStorageJSON(t, r, http.MethodDelete,
		fmt.Sprintf("/api/v1/licenses/%d?company_id=%d", licenseID, companyID), token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("pool-linked license delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 解除挂接后许可可删
	if err := store.DB.Model(&model.SoftwarePool{}).Where("id = ?", pool.ID).
		Update("license_id", nil).Error; err != nil {
		t.Fatalf("unlink pool: %v", err)
	}
	rec = doStorageJSON(t, r, http.MethodDelete,
		fmt.Sprintf("/api/v1/licenses/%d?company_id=%d", licenseID, companyID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("unlinked license delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}
