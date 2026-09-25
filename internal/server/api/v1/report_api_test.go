package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// P2 报表中心：gin 集成测试。
// 覆盖：汇总 8 卡（状态计数 + 在册口径价值合计）、年度资产价值窗口、
// 月度建账趋势、状态/类别分布环图、admin-only 角色守卫、公司边界

func setupReportRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/report_api_test.db")
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
	RegisterReportRoutes(protected)
	return r
}

// seedReportFixture 公司 A 五台资产（覆盖状态/列管/价值/年度/月度各维度）
// + 公司 B 一台资产（隔离断言用）
func seedReportFixture(t *testing.T) (companyID, otherCompanyID, adminID, userID int64) {
	t.Helper()
	company := model.Company{Name: "报表测试公司-" + t.Name()}
	other := model.Company{Name: "报表外公司-" + t.Name()}
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

	now := time.Now().UTC()
	mustAsset := func(status int, categoryID int64, purchase *time.Time, original, net float64, offBook bool, createdMonthsAgo int) model.Asset {
		t.Helper()
		a := model.Asset{
			CompanyID: company.ID, CategoryID: categoryID,
			CategoryName: model.AssetCategoryName(categoryID),
			AssetTag:     fmt.Sprintf("AST-RPT-%d-%d", status, time.Now().UnixNano()),
			Status:       status, OriginalPrice: original, NetValue: net, OffBook: offBook,
		}
		a.PurchaseDate = purchase
		if err := store.DB.Create(&a).Error; err != nil {
			t.Fatalf("seed asset: %v", err)
		}
		if createdMonthsAgo > 0 {
			if err := store.DB.Model(&model.Asset{}).Where("id = ?", a.ID).
				UpdateColumn("created_at", now.AddDate(0, -createdMonthsAgo, 0)).Error; err != nil {
				t.Fatalf("backdate asset: %v", err)
			}
		}
		return a
	}
	date := func(year, month int) *time.Time {
		d := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		return &d
	}
	_ = mustAsset(model.AssetStatusInUse, model.AssetCategoryPC, date(2026, 3), 10000, 8000, false, 0)   // 在用
	_ = mustAsset(model.AssetStatusStock, model.AssetCategoryNotebook, date(2025, 6), 5000, 3000, false, 3) // 库存，3 个月前建账
	_ = mustAsset(model.AssetStatusRepair, model.AssetCategoryMonitor, date(2025, 11), 2000, 1000, false, 1) // 维修
	_ = mustAsset(model.AssetStatusScrapped, model.AssetCategoryPeripheral, date(2026, 1), 1000, 0, false, 0) // 报废
	_ = mustAsset(model.AssetStatusInUse, model.AssetCategoryPC, date(2024, 1), 4000, 500, true, 0)          // 列管（off_book）

	isolated := model.Asset{
		CompanyID: other.ID, CategoryID: 1, AssetTag: "AST-RPT-OTHER",
		Status: model.AssetStatusInUse, OriginalPrice: 77777,
	}
	if err := store.DB.Create(&isolated).Error; err != nil {
		t.Fatalf("seed isolated asset: %v", err)
	}
	return company.ID, other.ID, admin.ID, user.ID
}

func TestReportSummaryCards(t *testing.T) {
	r := setupReportRouter(t)
	companyID, otherCompanyID, adminID, userID := seedReportFixture(t)
	admin := userTokenFor(t, adminID, "itadmin", "admin")
	user := userTokenFor(t, userID, "zhangsan", "user")

	// 角色守卫：普通用户 403
	for _, path := range []string{"/summary", "/annual-value", "/monthly-trend", "/distribution"} {
		rec := doDispatchJSON(t, r, http.MethodGet,
			fmt.Sprintf("/api/v1/reports%s?company_id=%d", path, companyID), user, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("user %s must 403, got %d", path, rec.Code)
		}
	}
	// company_id 必填
	rec := doDispatchJSON(t, r, http.MethodGet, "/api/v1/reports/summary", admin, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing company_id must 400, got %d", rec.Code)
	}

	data := getPortalJSON(t, r, fmt.Sprintf("/api/v1/reports/summary?company_id=%d", companyID), admin)
	// 8 卡：总数 5（含报废）、在用 2、库存 1、维修 1、报废 1、列管 1
	expect := map[string]float64{
		"total_assets": 5, "in_use": 2, "stock": 1, "repair": 1,
		"scrapped": 1, "off_book": 1,
		// 价值卡 = 在册口径（非报废）：10000+5000+2000+4000 / 8000+3000+1000+500
		"total_value": 21000, "net_value": 12500,
	}
	for key, want := range expect {
		got, ok := data[key].(float64)
		if !ok || got != want {
			t.Fatalf("summary %s: got %v want %v (all=%v)", key, data[key], want, data)
		}
	}
	// 公司边界：外公司资产不泄漏
	otherData := getPortalJSON(t, r, fmt.Sprintf("/api/v1/reports/summary?company_id=%d", otherCompanyID), admin)
	if otherData["total_assets"].(float64) != 1 {
		t.Fatalf("isolated company must see 1 asset: %+v", otherData)
	}
}

func TestReportAnnualValueAndTrend(t *testing.T) {
	r := setupReportRouter(t)
	companyID, _, adminID, _ := seedReportFixture(t)
	admin := userTokenFor(t, adminID, "itadmin", "admin")

	// 年度资产价值（3 年窗口，非报废口径）：2026/2025/2024 三行降序
	rec := doDispatchJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/reports/annual-value?company_id=%d&years=3", companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("annual-value: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var annual struct {
		Data struct {
			Rows []reportAnnualRow `json:"rows"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &annual)
	if len(annual.Data.Rows) != 3 {
		t.Fatalf("annual rows: %+v", annual.Data.Rows)
	}
	want := []reportAnnualRow{
		{Year: time.Now().UTC().Year(), Count: 1, Original: 10000, Net: 8000},
		{Year: time.Now().UTC().Year() - 1, Count: 2, Original: 7000, Net: 4000},
		{Year: time.Now().UTC().Year() - 2, Count: 1, Original: 4000, Net: 500},
	}
	for i := range want {
		if annual.Data.Rows[i] != want[i] {
			t.Fatalf("annual row %d: got %+v want %+v", i, annual.Data.Rows[i], want[i])
		}
	}

	// 月度建账趋势（4 个月窗口）：本月 3 台、1 个月前 1 台、2 个月前 0、3 个月前 1
	rec = doDispatchJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/reports/monthly-trend?company_id=%d&months=4", companyID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("monthly-trend: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var trend struct {
		Data struct {
			Rows []reportTrendRow `json:"rows"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &trend)
	if len(trend.Data.Rows) != 4 {
		t.Fatalf("trend rows: %+v", trend.Data.Rows)
	}
	last := trend.Data.Rows[len(trend.Data.Rows)-1]
	prev := trend.Data.Rows[len(trend.Data.Rows)-2]
	if last.Count != 3 || prev.Count != 1 || trend.Data.Rows[len(trend.Data.Rows)-3].Count != 0 {
		t.Fatalf("trend counts: %+v", trend.Data.Rows)
	}
}

func TestReportDistribution(t *testing.T) {
	r := setupReportRouter(t)
	companyID, _, adminID, _ := seedReportFixture(t)
	admin := userTokenFor(t, adminID, "itadmin", "admin")

	// 状态分布：使用中 2（40%）居首
	data := getPortalJSON(t, r, fmt.Sprintf("/api/v1/reports/distribution?company_id=%d", companyID), admin)
	rows := data["rows"].([]any)
	if len(rows) != 4 {
		t.Fatalf("status distribution rows: %+v", rows)
	}
	top := rows[0].(map[string]any)
	if top["label"] != "使用中" || top["count"].(float64) != 2 || top["percent"].(float64) != 40 {
		t.Fatalf("top status row: %+v", top)
	}

	// 类别分布：台式整机 2（40%）
	data = getPortalJSON(t, r, fmt.Sprintf("/api/v1/reports/distribution?company_id=%d&dimension=category", companyID), admin)
	rows = data["rows"].([]any)
	top = rows[0].(map[string]any)
	if top["label"] != "台式整机" || top["count"].(float64) != 2 || top["percent"].(float64) != 40 {
		t.Fatalf("top category row: %+v", top)
	}

	// 非法维度 400
	rec := doDispatchJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/reports/distribution?company_id=%d&dimension=bogus", companyID), admin, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus dimension must 400, got %d", rec.Code)
	}
}
