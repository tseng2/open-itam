package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/model"
)

// P2 体验运营 · 操作日志审计中间件：挂在 JWT 认证之后，对所有变更类
// 请求（POST/PUT/DELETE/PATCH）在业务完成后留痕——谁、何时、对什么对象、
// 做了什么动作、结果如何、从哪台终端。审计与业务解耦：落库失败只走 gin
// 错误链上报，绝不阻塞业务响应（沿 AssetEvent 联动"失败不回滚主流程"先例）。

// OperationLogSink 审计落库出口：小接口，生产注入 GormStore，
// 单测注入假实现，中间件不与具体存储耦合
type OperationLogSink interface {
	CreateOperationLog(ctx context.Context, log model.OperationLog) error
}

// 明细留档与报文预读上限：请求体摘要截断防大报文拖库；
// 超过预读上限的报文（未知分块长度的除外）不动流、明细留空
const (
	auditDetailLimit = 2048
	auditBodyMaxRead = 64 << 10
)

// ParseAuditRequest 从方法与路径派生审计三要素（动作/对象/对象 ID）。
// 规则：路径尾段为数字 → 方法缺省动作（POST=create / PUT=update /
// DELETE=delete）；尾段非数字 → 尾段即动作词（/dispatches/5/return →
// return、/assets/import → import），动作段的前一段为数字即对象 ID。
// 多级子资源（/assets/123/events/45/approve）的 ID 语义以 Path 原文兜底
func ParseAuditRequest(method, path string) (action, resource, resourceID string) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	// 只服务受保护组的 /api/v1/* 形态；无资源段（裸根）不留痕
	if len(segments) >= 2 && segments[0] == "api" && segments[1] == "v1" {
		segments = segments[2:]
	}
	if len(segments) == 0 || segments[0] == "" {
		return "", "", ""
	}
	resource = segments[0]
	last := segments[len(segments)-1]
	if isNumeric(last) {
		return defaultAuditAction(method), resource, last
	}
	if len(segments) >= 2 && isNumeric(segments[len(segments)-2]) {
		resourceID = segments[len(segments)-2]
	}
	if len(segments) >= 2 {
		return last, resource, resourceID
	}
	return defaultAuditAction(method), resource, ""
}

func defaultAuditAction(method string) string {
	switch method {
	case http.MethodPost:
		return model.OperationActionCreate
	case http.MethodPut, http.MethodPatch:
		return model.OperationActionUpdate
	case http.MethodDelete:
		return model.OperationActionDelete
	}
	return strings.ToLower(method)
}

func isNumeric(s string) bool {
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}

// AuditLog 变更类请求审计：挂在 AuthMiddleware 之后，从 JWT 上下文取
// 操作人身份；JSON 请求体预读作明细留档（读后复位，业务 handler 无感）
func AuditLog(sink OperationLogSink) gin.HandlerFunc {
	return func(c *gin.Context) {
		detail, bodyCompanyID := captureAuditBody(c)
		c.Next()
		if !isMutatingMethod(c.Request.Method) {
			return
		}
		action, resource, resourceID := ParseAuditRequest(c.Request.Method, c.Request.URL.Path)
		if resource == "" {
			return
		}
		log := model.OperationLog{
			CompanyID:  resolveAuditCompanyID(c, bodyCompanyID),
			UserID:     auditContextInt64(c, "userID"),
			Username:   auditContextString(c, "username"),
			Role:       auditContextString(c, "role"),
			Action:     action,
			Resource:   resource,
			ResourceID: resourceID,
			Path:       truncateAudit(c.Request.URL.Path, 255),
			Detail:     detail,
			IP:         c.ClientIP(),
			UserAgent:  truncateAudit(c.Request.UserAgent(), 255),
			Status:     c.Writer.Status(),
		}
		if err := sink.CreateOperationLog(c.Request.Context(), log); err != nil {
			_ = c.Error(err) // 审计失败不阻塞业务响应
		}
	}
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

// captureAuditBody 预读 JSON 请求体：明细截断留档 + 解析 body 里的
// company_id（query 缺席时的公司归属兜底）。读后复位请求体供业务消费；
// multipart（Excel 导入等）与超限报文一律不动流、明细留空。
// 仅处理 Content-Length 明确且 ≤ 上限的 JSON 报文——分块未知长度的
// 请求无法安全"读后复位"，宁可不留明细也不破坏业务
func captureAuditBody(c *gin.Context) (detail string, companyID int64) {
	if c.Request.Body == nil || c.Request.ContentLength <= 0 || c.Request.ContentLength > auditBodyMaxRead {
		return "", 0
	}
	if !strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
		return "", 0
	}
	raw, err := io.ReadAll(c.Request.Body)
	// 无论解析成败都复位请求体，保证业务 handler 拿到原始报文
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return "", 0
	}
	var probe struct {
		CompanyID int64 `json:"company_id"`
	}
	_ = json.Unmarshal(raw, &probe)
	return truncateAudit(strings.TrimSpace(string(raw)), auditDetailLimit), probe.CompanyID
}

// resolveAuditCompanyID 公司归属：query 的 company_id 优先（删除/列表
// 惯例），缺席时回落 body 解析值；两处都没有（全局配置类操作）记 0
func resolveAuditCompanyID(c *gin.Context, bodyCompanyID int64) int64 {
	if v, err := strconv.ParseInt(c.Query("company_id"), 10, 64); err == nil && v > 0 {
		return v
	}
	if bodyCompanyID > 0 {
		return bodyCompanyID
	}
	return 0
}

func auditContextInt64(c *gin.Context, key string) int64 {
	if v, ok := c.Get(key); ok {
		if n, ok := v.(int64); ok {
			return n
		}
	}
	return 0
}

func auditContextString(c *gin.Context, key string) string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func truncateAudit(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	// "…" 是 3 字节 UTF-8：预留后截断总长恰好等于上限
	return s[:limit-3] + "…"
}
