package v1

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/store"
)

// OperationLogHandler 操作日志查询面（P2 体验运营）：admin 只读。
// 写入由审计中间件（变更类请求）与登录处理器（认证事件）完成，
// 本面不提供任何修改/删除端点——审计流水追加后不可变

type OperationLogHandler struct {
	store *store.GormStore
}

func RegisterOperationLogRoutes(protected *gin.RouterGroup) {
	h := &OperationLogHandler{store: store.NewGormStore(store.DB)}
	adminOnly := protected.Group("/operation-logs")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.GET("", h.List)
	}
}

// operationLogListQuery 审计列表查询参数：company_id 0 = 全部（含全局）；
// start_time/end_time 为 RFC3339 闭区间
type operationLogListQuery struct {
	CompanyID  int64  `form:"company_id"`
	UserID     int64  `form:"user_id"`
	Keyword    string `form:"keyword"`
	Action     string `form:"action"`
	Resource   string `form:"resource"`
	ResourceID string `form:"resource_id"`
	StartTime  string `form:"start_time"`
	EndTime    string `form:"end_time"`
	Page       int    `form:"page,default=1"`
	PageSize   int    `form:"page_size,default=50"`
}

func (h *OperationLogHandler) List(c *gin.Context) {
	var q operationLogListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	f := store.OperationLogListFilter{
		CompanyID:  q.CompanyID,
		UserID:     q.UserID,
		Keyword:    q.Keyword,
		Action:     q.Action,
		Resource:   q.Resource,
		ResourceID: q.ResourceID,
		Page:       q.Page,
		PageSize:   q.PageSize,
	}
	if q.StartTime != "" {
		t, err := time.Parse(time.RFC3339, q.StartTime)
		if err != nil {
			Fail(c, http.StatusBadRequest, 40002, "start_time 需为 RFC3339 时间")
			return
		}
		f.StartTime = &t
	}
	if q.EndTime != "" {
		t, err := time.Parse(time.RFC3339, q.EndTime)
		if err != nil {
			Fail(c, http.StatusBadRequest, 40002, "end_time 需为 RFC3339 时间")
			return
		}
		f.EndTime = &t
	}
	items, total, err := h.store.ListOperationLogs(c.Request.Context(), f)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询操作日志失败")
		return
	}
	Success(c, PageResult{Total: total, Items: items})
}
