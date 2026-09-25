package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// P2 操作日志 gin 集成测试：审计中间件端到端留痕（真实业务写 + 越权失败
// 尝试）、admin 只读查询面（角色守卫 + 过滤 + 分页）、登录事件留痕。
// 走 InitDB + glebarez SQLite，GormStore 审计实现经此覆盖

func setupOperationLogRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := store.InitDB("sqlite", t.TempDir()+"/oplog_api_test.db")
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
	// 登录是公开认证面：留痕在登录处理器内单独落库
	RegisterAuthRoutes(apiV1)
	protected := apiV1.Group("/")
	protected.Use(middleware.AuthMiddleware())
	protected.Use(middleware.AuditLog(store.NewGormStore(store.DB)))
	RegisterOperationLogRoutes(protected)
	// 真实业务写面：用维度治理路由产生被审计的写请求
	RegisterDimensionRoutes(protected)
	return r
}

func doLoginJSON(t *testing.T, r http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func listOperationLogs(t *testing.T, r http.Handler, token, query string) (items []model.OperationLog, total int64) {
	t.Helper()
	rec := doDepJSON(t, r, "GET", "/api/v1/operation-logs"+query, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list op logs: http=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Total int64                  `json:"total"`
			Items []model.OperationLog   `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Data.Items, resp.Data.Total
}

func TestOperationLogAuditTrailAndListGuards(t *testing.T) {
	r := setupOperationLogRouter(t)
	admin := userToken(t, "admin")
	user := userToken(t, "user")
	companyID := seedDimensionCompany(t)

	// 读面守卫：user 角色禁止访问审计面
	rec := doDepJSON(t, r, "GET", "/api/v1/operation-logs", user, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin read must 403, got %d", rec.Code)
	}

	// 真实业务写（建厂商）→ 审计中间件端到端留痕
	rec = doDepJSON(t, r, "POST", "/api/v1/manufacturers", admin, map[string]any{
		"company_id": companyID, "name": "联想",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create manufacturer: http=%d body=%s", rec.Code, rec.Body.String())
	}
	// 越权写（user 写 admin-only 面）：403 失败尝试同样留痕
	rec = doDepJSON(t, r, "POST", "/api/v1/manufacturers", user, map[string]any{
		"company_id": companyID, "name": "x",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin write must 403, got %d", rec.Code)
	}

	items, total := listOperationLogs(t, r, admin, fmt.Sprintf("?company_id=%d", companyID))
	if total != 2 {
		t.Fatalf("expected 2 audit rows, got %d", total)
	}
	var created, denied *model.OperationLog
	for i := range items {
		switch {
		case items[i].Action == model.OperationActionCreate && items[i].Status == http.StatusOK:
			created = &items[i]
		case items[i].Action == model.OperationActionCreate && items[i].Status == http.StatusForbidden:
			denied = &items[i]
		}
	}
	if created == nil {
		t.Fatalf("create row missing: %+v", items)
	}
	if created.UserID != 1 || created.Username != "tester" || created.Role != "admin" ||
		created.Status != http.StatusOK || created.Detail == "" || created.Path != "/api/v1/manufacturers" {
		t.Fatalf("create row mismatch: %+v", created)
	}
	if denied == nil || denied.Action != model.OperationActionCreate || denied.Role != "user" {
		t.Fatalf("denied row missing or wrong: %+v", denied)
	}

	// 过滤：动作 + keyword + 时间窗；分页 total 全量
	items, total = listOperationLogs(t, r, admin,
		fmt.Sprintf("?company_id=%d&action=%s&keyword=tester", companyID, model.OperationActionCreate))
	if total != 2 || len(items) != 2 {
		t.Fatalf("action+keyword filter: total=%d len=%d", total, len(items))
	}
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	_, total = listOperationLogs(t, r, admin, "?start_time="+future)
	if total != 0 {
		t.Fatalf("future window must be empty, got %d", total)
	}
	_, total = listOperationLogs(t, r, admin, fmt.Sprintf("?company_id=%d&page=1&page_size=1", companyID))
	if total != 2 {
		t.Fatalf("pagination total: %d", total)
	}
	// 非法时间参数 → 400
	rec = doDepJSON(t, r, "GET", "/api/v1/operation-logs?start_time=not-a-time", admin, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad start_time must 400, got %d", rec.Code)
	}
}

func TestOperationLogLoginTrail(t *testing.T) {
	r := setupOperationLogRouter(t)
	companyID := seedDimensionCompany(t)

	// 查无此人的失败登录：user_id=0、company_id=0，仍留痕
	rec := doLoginJSON(t, r, map[string]any{"username": "ghost", "password": "x"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ghost login must 401, got %d", rec.Code)
	}
	// 真实用户密码错误：操作人身份取自用户表
	hash, err := bcrypt.GenerateFromPassword([]byte("right-pass"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u := model.User{CompanyID: companyID, Username: "victim", RealName: "李四",
		Status: "active", Role: "user", PasswordHash: string(hash)}
	if err := store.DB.Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	rec = doLoginJSON(t, r, map[string]any{"username": "victim", "password": "wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password must 401, got %d", rec.Code)
	}
	// 正确密码登录成功
	rec = doLoginJSON(t, r, map[string]any{"username": "victim", "password": "right-pass"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: http=%d body=%s", rec.Code, rec.Body.String())
	}

	admin := userToken(t, "admin")
	items, total := listOperationLogs(t, r, admin, "?resource=auth")
	if total != 3 {
		t.Fatalf("expected 3 login rows, got %d", total)
	}
	var failed, wrongPass, ok *model.OperationLog
	for i := range items {
		if items[i].Action == model.OperationActionLoginFailed {
			if items[i].Username == "ghost" {
				failed = &items[i]
			} else {
				wrongPass = &items[i]
			}
		} else if items[i].Action == model.OperationActionLogin {
			ok = &items[i]
		}
	}
	if failed == nil || failed.UserID != 0 || failed.CompanyID != 0 || failed.Status != http.StatusUnauthorized {
		t.Fatalf("ghost row mismatch: %+v", failed)
	}
	if wrongPass == nil || wrongPass.UserID != u.ID || wrongPass.CompanyID != companyID {
		t.Fatalf("wrong-password row must carry user identity: %+v", wrongPass)
	}
	if ok == nil || ok.Status != http.StatusOK || ok.UserID != u.ID || ok.Role != "user" {
		t.Fatalf("success row mismatch: %+v", ok)
	}
}
