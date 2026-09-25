package v1

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// P2 员工自助门户：普通员工的个人视角聚合面（CIYO PersonalStatsVO 对标）。
// 全部只读、JWT 本人收口（传 user_id/applicant_id 一律无效）——
// user_id 即安全边界（notifications 先例），无公司上下文。
// 我的设备含"使用中 + 维修中"（维修设备仍归属领用人），报废不算

// portalExpiringWindow 到期提醒窗口：已批短期借用 30 天内到期计入
const portalExpiringWindowDays = 30

type PortalHandler struct {
	store *store.GormStore
}

func RegisterPortalRoutes(protected *gin.RouterGroup) {
	h := &PortalHandler{store: store.NewGormStore(store.DB)}
	portal := protected.Group("/portal")
	{
		portal.GET("/summary", h.Summary)
		portal.GET("/my-assets", h.MyAssets)
		portal.GET("/my-requests", h.MyRequests)
	}
}

// myAssetStatuses 我的设备口径：使用中 + 维修中（报废不归员工视角）
func myAssetStatuses() []int {
	return []int{model.AssetStatusInUse, model.AssetStatusRepair}
}

func (h *PortalHandler) Summary(c *gin.Context) {
	userID := currentSelfID(c)
	if userID <= 0 {
		Fail(c, http.StatusBadRequest, 40002, "无法识别当前用户")
		return
	}
	ctx := c.Request.Context()
	now := time.Now().UTC()

	var deviceCount, pendingCount, expiringCount int64
	if err := store.DB.WithContext(ctx).Model(&model.Asset{}).
		Where("user_id = ? AND status IN ?", userID, myAssetStatuses()).
		Count(&deviceCount).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询我的设备失败")
		return
	}
	if err := store.DB.WithContext(ctx).Model(&model.AssetRequest{}).
		Where("applicant_id = ? AND status = ?", userID, model.AssetRequestStatusPending).
		Count(&pendingCount).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询我的申请失败")
		return
	}
	if err := store.DB.WithContext(ctx).Model(&model.AssetRequest{}).
		Where("applicant_id = ? AND status = ? AND expected_return_at IS NOT NULL AND expected_return_at >= ? AND expected_return_at <= ?",
			userID, model.AssetRequestStatusApproved, now, now.AddDate(0, 0, portalExpiringWindowDays)).
		Count(&expiringCount).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询归还提醒失败")
		return
	}

	// 持有天数：我的在册设备最早一次领用履历距今的整日数（无履历记 0）。
	// 用模型查询而非 MIN 聚合——glebarez 对 MIN() 结果返回原始字符串，
	// 模型字段的 schema 转换器才能正确解析时间文本
	daysInUse := 0
	var earliestEvent model.AssetEvent
	err := store.DB.WithContext(ctx).Model(&model.AssetEvent{}).
		Joins("JOIN assets ON assets.id = asset_events.asset_id").
		Where("assets.user_id = ? AND assets.status IN ? AND assets.deleted_at IS NULL AND asset_events.event_type = ?",
			userID, myAssetStatuses(), model.AssetEventAssign).
		Order("asset_events.created_at ASC").
		First(&earliestEvent).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		Fail(c, http.StatusInternalServerError, 50001, "查询持有履历失败")
		return
	}
	if err == nil {
		daysInUse = int(now.Sub(earliestEvent.CreatedAt).Hours() / 24)
		if daysInUse < 0 {
			daysInUse = 0
		}
	}

	Success(c, gin.H{
		"device_count":          deviceCount,
		"pending_request_count": pendingCount,
		"days_in_use":           daysInUse,
		"expiring_count":        expiringCount,
	})
}

func (h *PortalHandler) MyAssets(c *gin.Context) {
	userID := currentSelfID(c)
	if userID <= 0 {
		Fail(c, http.StatusBadRequest, 40002, "无法识别当前用户")
		return
	}
	page, pageSize := portalPageParams(c)

	q := store.DB.WithContext(c.Request.Context()).Model(&model.Asset{}).
		Where("user_id = ? AND status IN ?", userID, myAssetStatuses())
	var total int64
	if err := q.Count(&total).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询我的设备失败")
		return
	}
	items := make([]model.Asset, 0, pageSize)
	if err := q.Order("id desc").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询我的设备失败")
		return
	}
	// 维度富化与台账列表同口径（license_name/supplier_name 等）
	if err := enrichAssetsDimensions(c.Request.Context(), items); err != nil {
		_ = c.Error(err)
	}
	Success(c, PageResult{Total: total, Items: items})
}

// MyRequests 我的设备申请（全部状态）；申请人强制为 JWT 本人
//（CompanyID 0 = 不限公司，本人即边界）
func (h *PortalHandler) MyRequests(c *gin.Context) {
	userID := currentSelfID(c)
	if userID <= 0 {
		Fail(c, http.StatusBadRequest, 40002, "无法识别当前用户")
		return
	}
	page, pageSize := portalPageParams(c)
	items, total, err := h.store.ListAssetRequests(c.Request.Context(), store.AssetRequestListFilter{
		ApplicantID: userID, Page: page, PageSize: pageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询我的申请失败")
		return
	}
	Success(c, PageResult{Total: total, Items: items})
}

// portalPageParams 门户分页兜底：非法/缺席取默认，页宽上限 100
func portalPageParams(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
