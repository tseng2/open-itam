package middleware

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/model"
)

// P2 操作日志：审计中间件测试。
// 覆盖：路径解析纯函数（动作/对象/对象 ID 派生规则）、变更类请求留痕、
// 请求体预读与复位、公司归属解析（query 优先 body 兜底）、GET 不留痕、
// 越权失败尝试留痕、审计落库故障不阻塞业务、multipart/超限报文不采集

// fakeAuditSink 收集中间件产出的审计记录，可注入落库故障
type fakeAuditSink struct {
	logs []model.OperationLog
	err  error
}

func (f *fakeAuditSink) CreateOperationLog(_ context.Context, log model.OperationLog) error {
	if f.err != nil {
		return f.err
	}
	f.logs = append(f.logs, log)
	return nil
}

func setupAuditRouter(sink *fakeAuditSink, echoBodyLen *int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	protected := r.Group("/api/v1")
	protected.Use(AuthMiddleware())
	protected.Use(AuditLog(sink))
	{
		protected.POST("/assets", func(c *gin.Context) {
			raw, _ := c.GetRawData()
			if echoBodyLen != nil {
				*echoBodyLen = len(raw)
			}
			c.JSON(http.StatusOK, gin.H{"code": 0})
		})
		protected.PUT("/assets/:id", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
		protected.DELETE("/assets/:id", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
		protected.GET("/assets", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
		adminOnly := protected.Group("/")
		adminOnly.Use(RoleMiddleware("admin"))
		{
			adminOnly.POST("/dispatches", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
		}
	}
	return r
}

func auditDo(r *gin.Engine, method, path, token string, body []byte, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	} else if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "audit-test-agent")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestParseAuditRequest(t *testing.T) {
	cases := []struct {
		method, path          string
		action, resource, resID string
	}{
		{http.MethodPost, "/api/v1/assets", model.OperationActionCreate, "assets", ""},
		{http.MethodPost, "/api/v1/assets/", model.OperationActionCreate, "assets", ""},
		{http.MethodPut, "/api/v1/assets/5", model.OperationActionUpdate, "assets", "5"},
		{http.MethodDelete, "/api/v1/manufacturers/3", model.OperationActionDelete, "manufacturers", "3"},
		{http.MethodPost, "/api/v1/dispatches/5/return", "return", "dispatches", "5"},
		{http.MethodPost, "/api/v1/asset-requests/9/cancel", "cancel", "asset-requests", "9"},
		{http.MethodPost, "/api/v1/assets/import", "import", "assets", ""},
		{http.MethodPost, "/api/v1/depreciations/recalculate", "recalculate", "depreciations", ""},
		{http.MethodPost, "/api/v1/assets/7/off-book", "off-book", "assets", "7"},
		{http.MethodPut, "/api/v1/protection/modules/quit", "quit", "protection", ""},
		{http.MethodPut, "/api/v1/webhook-alerts/config", "config", "webhook-alerts", ""},
		{http.MethodPost, "/api/v1/stocktakes/9/items", "items", "stocktakes", "9"},
		{http.MethodPost, "/api/v1/assets/123/events/45/approve", "approve", "assets", "45"},
	}
	for _, tc := range cases {
		action, resource, resID := ParseAuditRequest(tc.method, tc.path)
		if action != tc.action || resource != tc.resource || resID != tc.resID {
			t.Errorf("%s %s = (%q,%q,%q), want (%q,%q,%q)",
				tc.method, tc.path, action, resource, resID, tc.action, tc.resource, tc.resID)
		}
	}
}

func TestAuditLogCapturesMutatingRequests(t *testing.T) {
	sink := &fakeAuditSink{}
	var handlerBodyLen int
	r := setupAuditRouter(sink, &handlerBodyLen)
	token, err := GenerateToken(7, "张三", "admin")
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	// POST：query company_id 优先，JSON 体预读后复位（handler 仍能读全）
	rec := auditDo(r, http.MethodPost, "/api/v1/assets?company_id=3", token,
		[]byte(`{"name":"x","company_id":9}`), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("post: http=%d", rec.Code)
	}
	if handlerBodyLen != len(`{"name":"x","company_id":9}`) {
		t.Fatalf("body must be restored for handler, got len=%d", handlerBodyLen)
	}
	if len(sink.logs) != 1 {
		t.Fatalf("one log expected, got %d", len(sink.logs))
	}
	got := sink.logs[0]
	if got.UserID != 7 || got.Username != "张三" || got.Role != "admin" {
		t.Fatalf("operator mismatch: %+v", got)
	}
	if got.Action != model.OperationActionCreate || got.Resource != "assets" || got.ResourceID != "" {
		t.Fatalf("classification mismatch: %+v", got)
	}
	if got.CompanyID != 3 {
		t.Fatalf("query company_id must win, got %d", got.CompanyID)
	}
	if got.Status != http.StatusOK || got.IP == "" || got.UserAgent != "audit-test-agent" {
		t.Fatalf("request meta mismatch: %+v", got)
	}
	if got.Path != "/api/v1/assets" || !strings.Contains(got.Detail, `"name"`) {
		t.Fatalf("path/detail mismatch: %+v", got)
	}

	// PUT：无 query 时 company_id 取自 body；resource_id 取自路径
	auditDo(r, http.MethodPut, "/api/v1/assets/5", token, []byte(`{"company_id":12}`), "")
	if len(sink.logs) != 2 {
		t.Fatalf("two logs expected, got %d", len(sink.logs))
	}
	put := sink.logs[1]
	if put.Action != model.OperationActionUpdate || put.ResourceID != "5" || put.CompanyID != 12 {
		t.Fatalf("put mismatch: %+v", put)
	}

	// DELETE：无 body 全局留痕（company 0）
	auditDo(r, http.MethodDelete, "/api/v1/assets/6", token, nil, "")
	if len(sink.logs) != 3 || sink.logs[2].Action != model.OperationActionDelete {
		t.Fatalf("delete mismatch: %+v", sink.logs)
	}

	// GET：读操作不留痕
	auditDo(r, http.MethodGet, "/api/v1/assets", token, nil, "")
	if len(sink.logs) != 3 {
		t.Fatalf("GET must not be logged, got %d", len(sink.logs))
	}
}

func TestAuditLogFailedAttemptAndSinkResilience(t *testing.T) {
	sink := &fakeAuditSink{}
	var handlerBodyLen int
	r := setupAuditRouter(sink, &handlerBodyLen)
	userToken, _ := GenerateToken(8, "李四", "user")

	// user 角色写 admin-only 端点：403 失败尝试也要留痕
	rec := auditDo(r, http.MethodPost, "/api/v1/dispatches", userToken,
		[]byte(`{"company_id":3}`), "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin must 403, got %d", rec.Code)
	}
	if len(sink.logs) != 1 || sink.logs[0].Status != http.StatusForbidden {
		t.Fatalf("failed attempt must be logged: %+v", sink.logs)
	}

	// 审计落库故障：业务响应不受影响
	sink.err = errors.New("db down")
	rec = auditDo(r, http.MethodPost, "/api/v1/assets?company_id=3", userToken,
		[]byte(`{}`), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sink failure must not break business, got %d", rec.Code)
	}

	// 未认证请求在 AuthMiddleware 就被拦下，不进审计（login_failed 由登录处理器单独留痕）
	auditDo(r, http.MethodPost, "/api/v1/assets", "", []byte(`{}`), "")
	if len(sink.logs) != 1 {
		t.Fatalf("unauthenticated must not hit audit, got %d", len(sink.logs))
	}
}

func TestAuditLogBodyCaptureGuards(t *testing.T) {
	sink := &fakeAuditSink{}
	var handlerBodyLen int
	r := setupAuditRouter(sink, &handlerBodyLen)
	token, _ := GenerateToken(1, "op", "admin")

	// multipart（Excel 导入等）：不采集明细、不动流
	rec := auditDo(r, http.MethodPost, "/api/v1/assets", token,
		[]byte("----boundary\r\ncontent"), "multipart/form-data; boundary=----boundary")
	if rec.Code != http.StatusOK {
		t.Fatalf("multipart post: %d", rec.Code)
	}
	if len(sink.logs) != 1 || sink.logs[0].Detail != "" {
		t.Fatalf("multipart must skip detail: %+v", sink.logs)
	}

	// 超限 JSON（> 64KB）：不采集，handler 仍可读完整报文
	big := `{"data":"` + strings.Repeat("x", 70*1024) + `"}`
	rec = auditDo(r, http.MethodPost, "/api/v1/assets", token, []byte(big), "")
	if rec.Code != http.StatusOK || handlerBodyLen != len(big) {
		t.Fatalf("oversized body must pass through, http=%d len=%d", rec.Code, handlerBodyLen)
	}
	if len(sink.logs) != 2 || sink.logs[1].Detail != "" {
		t.Fatalf("oversized detail must be empty: %+v", sink.logs)
	}

	// 截断：2KB 上限的 JSON 体只留前 2048 字节
	huge := `{"k":"` + strings.Repeat("y", 4000) + `"}`
	auditDo(r, http.MethodPost, "/api/v1/assets", token, []byte(huge), "")
	if len(sink.logs) != 3 {
		t.Fatal("third log expected")
	}
	if got := sink.logs[2].Detail; len(got) != auditDetailLimit {
		t.Fatalf("detail must be truncated to %d, got %d", auditDetailLimit, len(got))
	}
}
