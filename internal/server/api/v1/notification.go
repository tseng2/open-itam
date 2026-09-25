package v1

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// P2 消息中心起步（站内信）：个人收件箱端点 + 业务事件源的投递工具。
// 读/写全部以 JWT 本人身份收口（传 user_id 也无效）；company_id 可选
//（0 = 不限公司——顶栏铃铛无公司上下文，user_id 即安全边界）。
// 通知是业务旁路：推送失败只走 gin 错误链，绝不阻塞主流程

type NotificationHandler struct {
	store *store.GormStore
}

func RegisterNotificationRoutes(protected *gin.RouterGroup) {
	h := &NotificationHandler{store: store.NewGormStore(store.DB)}
	g := protected.Group("/notifications")
	{
		g.GET("", h.List)
		g.GET("/unread-count", h.UnreadCount)
		g.POST("/read-all", h.ReadAll)
		g.POST("/:id/read", h.Read)
	}
}

type notificationListQuery struct {
	CompanyID int64  `form:"company_id"` // 0 = 不限公司
	Unread    *bool  `form:"unread"`
	Type      string `form:"type"`
	Page      int    `form:"page,default=1"`
	PageSize  int    `form:"page_size,default=20"`
}

func (h *NotificationHandler) List(c *gin.Context) {
	var q notificationListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	items, total, err := h.store.ListNotifications(c.Request.Context(), store.NotificationListFilter{
		CompanyID: q.CompanyID,
		UserID:    currentSelfID(c), // 收件箱只查自己
		Unread:    q.Unread,
		Type:     q.Type,
		Page:      q.Page,
		PageSize:  q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询消息失败")
		return
	}
	Success(c, PageResult{Total: total, Items: items})
}

func (h *NotificationHandler) UnreadCount(c *gin.Context) {
	companyID, _ := strconv.ParseInt(c.Query("company_id"), 10, 64)
	n, err := h.store.CountUnreadNotifications(c.Request.Context(), companyID, currentSelfID(c))
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询未读消息失败")
		return
	}
	Success(c, gin.H{"count": n})
}

func (h *NotificationHandler) Read(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	companyID, _ := strconv.ParseInt(c.Query("company_id"), 10, 64)
	if err := h.store.MarkNotificationRead(c.Request.Context(), companyID, currentSelfID(c), id, time.Now().UTC()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			Fail(c, http.StatusNotFound, 40401, "消息不存在")
			return
		}
		Fail(c, http.StatusInternalServerError, 50001, "标记已读失败")
		return
	}
	Success(c, gin.H{"read": true})
}

func (h *NotificationHandler) ReadAll(c *gin.Context) {
	companyID, _ := strconv.ParseInt(c.Query("company_id"), 10, 64)
	affected, err := h.store.MarkAllNotificationsRead(c.Request.Context(), companyID, currentSelfID(c), time.Now().UTC())
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "全部已读失败")
		return
	}
	Success(c, gin.H{"affected": affected})
}

// ==================== 事件源投递工具（业务旁路）====================

// pushNotification 单条投递：失败经 c.Error 上报，不阻塞业务响应
//（沿审计旁路先例；通知语义永远弱于业务成功）
func pushNotification(c *gin.Context, n model.Notification) {
	s := store.NewGormStore(store.DB)
	if _, err := s.CreateNotification(c.Request.Context(), n); err != nil {
		_ = c.Error(fmt.Errorf("push notification %q: %w", n.Title, err))
	}
}

// notifyCompanyAdmins 待办类事件扇出：通知该公司全部在册管理员
//（admin + super_admin），申请人提交审批等场景使用；查不到管理员
// 只上报不报错（无人可通知是数据状态而非故障）
func notifyCompanyAdmins(c *gin.Context, tpl model.Notification) {
	var admins []model.User
	if err := store.DB.WithContext(c.Request.Context()).
		Where("company_id = ? AND role IN ? AND status = ?", tpl.CompanyID, []string{"admin", "super_admin"}, "active").
		Find(&admins).Error; err != nil {
		_ = c.Error(fmt.Errorf("list admins for notify: %w", err))
		return
	}
	for _, a := range admins {
		n := tpl
		n.UserID = a.ID
		pushNotification(c, n)
	}
}

// assetTagForNotify 通知文案的资产编码兜底：富化查询失败时回落
//"资产 #ID"，通知不因文案富化失败而丢失（旁路语义一以贯之）
func assetTagForNotify(c *gin.Context, assetID int64) string {
	var a model.Asset
	if err := store.DB.WithContext(c.Request.Context()).
		Select("asset_tag").First(&a, assetID).Error; err != nil {
		return fmt.Sprintf("资产 #%d", assetID)
	}
	return a.AssetTag
}
