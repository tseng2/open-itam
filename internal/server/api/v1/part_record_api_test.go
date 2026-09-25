package v1

import (
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

// 配件出入库 API 契约测试（2026-09-26 补齐 + RBAC 收口）：
// 追加式流水无编辑删除面；direction 必填 in/out；operated_at 缺省服务端 now；
// 操作人服务端取 JWT；列表 direction/part_type 精确 + asset_tag 模糊

type partRecordResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type partRecordPage struct {
	Total int64               `json:"total"`
	Items []model.PartRecord `json:"items"`
}

func setupPartRecordRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/part_record_test.db")
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
	RegisterPartRecordRoutes(protected)
	return r
}

func seedPartRecordCompany(t *testing.T) int64 {
	t.Helper()
	company := model.Company{Name: "配件流水测试公司-" + t.Name()}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	return company.ID
}

func decodePartRecord(t *testing.T, rec *httptest.ResponseRecorder) model.PartRecord {
	t.Helper()
	var resp partRecordResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("resp decode: err=%v code=%d body=%s", err, resp.Code, rec.Body.String())
	}
	var item model.PartRecord
	if err := json.Unmarshal(resp.Data, &item); err != nil {
		t.Fatalf("decode part record data: %v", err)
	}
	return item
}

func TestPartRecordCreateListFlow(t *testing.T) {
	r := setupPartRecordRouter(t)
	companyID := seedPartRecordCompany(t)
	token := adminToken(t)

	rec := doStorageJSON(t, r, http.MethodPost, "/api/v1/part-records", token, gin.H{
		"company_id":      companyID,
		"direction":       "in",
		"part_type":      "内存",
		"part_name":      "DDR4 16G 笔记本内存",
		"part_model":     "三星 M471A1K43DB1",
		"brand":          "三星",
		"quantity":       5,
		"unit":           "条",
		"locker_location": "IT 柜 B-03",
		"purpose":        "项目备件",
		"oa_number":      "OA-2026-001",
		"location":       "3 楼机房备件区",
		"asset_tag":      "AST-PR-001",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	created := decodePartRecord(t, rec)
	if created.Direction != model.PartDirectionIn || created.Quantity != 5 {
		t.Fatalf("unexpected created record: %+v", created)
	}
	// operated_at 缺省服务端 now；操作人取 JWT（userID=1）
	if created.OperatedAt == nil || created.OperatorID == nil || *created.OperatorID != 1 {
		t.Fatalf("operated_at/operator must be server-side: %+v", created)
	}

	// direction 非法值 → 400
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/part-records", token, gin.H{
		"company_id": companyID, "direction": "sideways", "part_type": "内存",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad direction status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 出库流水 + quantity 缺省 1
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/part-records", token, gin.H{
		"company_id": companyID, "direction": "out", "part_type": "内存",
		"part_name": "DDR4 16G", "asset_tag": "AST-PR-001",
	})
	out := decodePartRecord(t, rec)
	if out.Direction != model.PartDirectionOut || out.Quantity != 1 {
		t.Fatalf("unexpected out record: %+v", out)
	}

	// 列表：direction/part_type 精确
	rec = doStorageJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/part-records?company_id=%d&direction=in&part_type=内存", companyID), token, nil)
	var resp partRecordResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Code != 0 {
		t.Fatalf("list resp decode: err=%v code=%d", err, resp.Code)
	}
	var page partRecordPage
	if err := json.Unmarshal(resp.Data, &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("direction/part_type exact filter: total=%d items=%+v", page.Total, page.Items)
	}

	// asset_tag 模糊
	rec = doStorageJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/part-records?company_id=%d&asset_tag=PR-00", companyID), token, nil)
	var resp2 partRecordResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp2); err != nil || resp2.Code != 0 {
		t.Fatalf("list resp2 decode: err=%v code=%d", err, resp2.Code)
	}
	var page2 partRecordPage
	if err := json.Unmarshal(resp2.Data, &page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if page2.Total != 2 {
		t.Fatalf("asset_tag fuzzy filter: total=%d", page2.Total)
	}
}

// 写面收口 admin（2026-09-26）：user 登记 403，读面保持登录可读
func TestPartRecordWriteRequiresAdminRole(t *testing.T) {
	r := setupPartRecordRouter(t)
	companyID := seedPartRecordCompany(t)
	token, err := middleware.GenerateToken(2, "ordinary", "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	rec := doStorageJSON(t, r, http.MethodGet, fmt.Sprintf("/api/v1/part-records?company_id=%d", companyID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user read status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doStorageJSON(t, r, http.MethodPost, "/api/v1/part-records", token, gin.H{
		"company_id": companyID, "direction": "in", "part_type": "内存",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user create status=%d body=%s", rec.Code, rec.Body.String())
	}
}
