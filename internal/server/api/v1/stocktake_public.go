package v1

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// maxPublicCheckBatch 移动端单批核对上限：一次扫码通常 1 条，
// 上限只用于批量补录，防单请求滥用
const maxPublicCheckBatch = 100

// StocktakePublicHandler 免登录移动扫码面（阶段五 P0-β，项目首个非 JWT 面）。
// 安全模型：
//   - 鉴权 = 盘点任务扫码令牌（128-bit 随机，库中只存 SHA-256，
//     仅任务"盘点中"有效，finish/cancel 即失效，泄露可轮换）
//   - 限流 = 按 IP 固定窗口（ratelimit.go），压制令牌枚举与轰炸
//   - 信息 = 资产只回白名单字段（无价格/备注/领用人等台账敏感信息），
//     任务不存在与非盘点中同样返回 404，不向扫码方泄露任务状态
type StocktakePublicHandler struct {
	store *store.GormStore
}

// RegisterStocktakePublicRoutes 免登录移动扫码路由（默认限流）
func RegisterStocktakePublicRoutes(apiV1 *gin.RouterGroup) {
	RegisterStocktakePublicRoutesWithLimit(apiV1, middleware.DefaultPublicRateLimitPerMinute)
}

// RegisterStocktakePublicRoutesWithLimit 同上，限流阈值可注入（测试用）
func RegisterStocktakePublicRoutesWithLimit(apiV1 *gin.RouterGroup, perMinute int) {
	pub := apiV1.Group("/public/stocktakes")
	pub.Use(middleware.PublicRateLimit(perMinute))
	h := &StocktakePublicHandler{store: store.NewGormStore(store.DB)}
	{
		pub.GET("/:token", h.Summary)
		pub.GET("/:token/assets/:tag", h.Asset)
		pub.POST("/:token/check", h.Check)
	}
}

// publicStocktakeSummary 盘点任务概要：任务名 + 各结果段位计数
type publicStocktakeSummary struct {
	ID     int64          `json:"id"`
	Name   string         `json:"name"`
	Counts map[int]int64  `json:"counts"`
}

func (h *StocktakePublicHandler) resolveTask(c *gin.Context) (model.Stocktake, bool) {
	st, err := h.store.GetStocktakeByToken(c.Request.Context(), c.Param("token"))
	if err != nil {
		Fail(c, http.StatusNotFound, 40401, "盘点码无效或盘点已结束")
		return model.Stocktake{}, false
	}
	return st, true
}

func (h *StocktakePublicHandler) Summary(c *gin.Context) {
	st, ok := h.resolveTask(c)
	if !ok {
		return
	}
	counts, err := h.store.CountStocktakeResults(c.Request.Context(), st.CompanyID, st.ID)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询盘点进度失败")
		return
	}
	Success(c, publicStocktakeSummary{ID: st.ID, Name: st.Name, Counts: counts})
}

// publicStocktakeAsset 资产白名单视图：扫码人核对所需的识别信息；
// 严禁加入价格、备注、部门/领用人等台账敏感字段
type publicStocktakeAsset struct {
	AssetTag     string `json:"asset_tag"`
	CategoryName string `json:"category_name"`
	Brand        string `json:"brand"`
	ModelName    string `json:"model_name"`
	SerialNumber string `json:"serial_number"`
	Location     string `json:"location"`
	ManagerName  string `json:"manager_name"`
	Status       int    `json:"status"`
}

// publicStocktakeAssetResp 扫码详情响应：范围外资产只回 tag 与 in_scope=false，
// 不泄露任何台账信息
type publicStocktakeAssetResp struct {
	Asset    publicStocktakeAsset `json:"asset"`
	Item     *model.StocktakeItem `json:"item"`
	InScope  bool                 `json:"in_scope"`
	HwPending int64               `json:"hw_pending"` // 待确认硬件变更数（核对"正常"时自动确认——A3 协同）
}

func (h *StocktakePublicHandler) Asset(c *gin.Context) {
	st, ok := h.resolveTask(c)
	if !ok {
		return
	}
	tag := c.Param("tag")

	var item model.StocktakeItem
	err := store.DB.WithContext(c.Request.Context()).
		Where("stocktake_id = ? AND company_id = ? AND asset_tag = ?", st.ID, st.CompanyID, tag).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 范围外：区分"资产不存在"与"不在本次盘点范围"两类提示，
		// 但对范围外资产只回 tag 本身，不泄露台账信息
		var exists int64
		if err := store.DB.WithContext(c.Request.Context()).Model(&model.Asset{}).
			Where("company_id = ? AND asset_tag = ?", st.CompanyID, tag).
			Count(&exists).Error; err != nil || exists == 0 {
			Fail(c, http.StatusNotFound, 40404, "资产不存在")
			return
		}
		Success(c, publicStocktakeAssetResp{
			Asset:   publicStocktakeAsset{AssetTag: tag},
			InScope: false,
		})
		return
	}
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询盘点明细失败")
		return
	}

	var asset model.Asset
	if err := store.DB.WithContext(c.Request.Context()).
		Where("company_id = ? AND id = ?", st.CompanyID, item.AssetID).
		First(&asset).Error; err != nil {
		Fail(c, http.StatusNotFound, 40404, "资产不存在")
		return
	}

	// A3 协同展示：该资产 pending 的硬件变更数，提示扫码人现场核实后再判"正常"
	var hwPending int64
	if err := store.DB.WithContext(c.Request.Context()).Model(&model.AssetEvent{}).
		Where("asset_id = ? AND event_type = ? AND review_status = ?",
			item.AssetID, model.AssetEventHardwareChange, model.AssetEventReviewPending).
		Count(&hwPending).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "查询硬件变更失败")
		return
	}

	Success(c, publicStocktakeAssetResp{
		Asset: publicStocktakeAsset{
			AssetTag:     asset.AssetTag,
			CategoryName: asset.CategoryName,
			Brand:        asset.Brand,
			ModelName:    asset.ModelName,
			SerialNumber: asset.SerialNumber,
			Location:     asset.Location,
			ManagerName:  asset.ManagerName,
			Status:       asset.Status,
		},
		Item:      &item,
		InScope:   true,
		HwPending: hwPending,
	})
}

type publicStocktakeCheckRequest struct {
	ScannedBy string                  `json:"scanned_by" binding:"required"`
	Checks    []model.StocktakeCheck  `json:"checks" binding:"required,min=1"`
}

func (h *StocktakePublicHandler) Check(c *gin.Context) {
	st, ok := h.resolveTask(c)
	if !ok {
		return
	}
	var req publicStocktakeCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if len(req.Checks) > maxPublicCheckBatch {
		Fail(c, http.StatusBadRequest, 40002, "单次最多核对 100 条")
		return
	}
	// 公开面一律按扫码 tag 定位：不支持 item_id，缩小可探测面
	for i := range req.Checks {
		req.Checks[i].ItemID = 0
	}

	items, err := h.store.CheckStocktakeItems(c.Request.Context(), st.CompanyID, st.ID, req.Checks, req.ScannedBy)
	if err != nil {
		failStocktakeCheckError(c, err)
		return
	}
	applyStocktakeSideEffects(c, st, items, req.ScannedBy, nil)
	Success(c, gin.H{"items": items})
}
