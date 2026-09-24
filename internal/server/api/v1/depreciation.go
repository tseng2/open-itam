package v1

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/depreciation"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// DepreciationHandler 折旧规则引擎管理面（阶段五 P0-β）：
// 规则 CRUD + 手动重算净值 + 资产财务销账/恢复（列管资产支持）。
// 计算口径收口在 internal/server/depreciation 纯函数包，此处只做装配
type DepreciationHandler struct {
	store  *store.GormStore
	engine *depreciation.Engine
}

// RegisterDepreciationRoutes 注册折旧管理路由。
// 读面开放给所有登录用户（资产表单的规则下拉与名称映射依赖它）；
// 写面与销账/恢复属财务动作，仅 admin；与资产路由同前缀分两组注册，
// 各自方法树不冲突（asset-requests 先例）
func RegisterDepreciationRoutes(protected *gin.RouterGroup) {
	h := &DepreciationHandler{
		store:  store.NewGormStore(store.DB),
		engine: depreciation.NewEngine(store.DB),
	}
	read := protected.Group("/depreciations")
	{
		read.GET("", h.ListRules)
		read.GET("/:id", h.GetRule)
	}
	adminOnly := protected.Group("/depreciations")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.POST("", h.CreateRule)
		adminOnly.PUT("/:id", h.UpdateRule)
		adminOnly.DELETE("/:id", h.DeleteRule)
		adminOnly.POST("/recalculate", h.Recalculate)
	}
	// 列管资产：财务销账/恢复挂接在资产动作面（off_book 与运营状态正交）
	finance := protected.Group("/assets")
	finance.Use(middleware.RoleMiddleware("admin"))
	{
		finance.POST("/:id/off-book", h.OffBook)
		finance.POST("/:id/restore-book", h.RestoreBook)
	}
}

type listDepreciationQuery struct {
	CompanyID int64 `form:"company_id" binding:"required"`
	Page      int   `form:"page,default=1"`
	PageSize  int   `form:"page_size,default=50"`
}

func (h *DepreciationHandler) ListRules(c *gin.Context) {
	var q listDepreciationQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	items, total, err := h.store.ListDepreciationRules(c.Request.Context(), store.DepreciationRuleListFilter{
		CompanyID: q.CompanyID,
		Page:      q.Page,
		PageSize:  q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询折旧规则失败")
		return
	}
	Success(c, PageResult{Total: total, Items: items})
}

func (h *DepreciationHandler) GetRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	companyID, err := strconv.ParseInt(c.Query("company_id"), 10, 64)
	if err != nil || companyID <= 0 {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	r, err := h.store.GetDepreciationRule(c.Request.Context(), companyID, id)
	if err != nil {
		failDepreciationStoreError(c, err, "查询折旧规则失败")
		return
	}
	Success(c, r)
}

// depreciationRuleRequest 创建/更新共用请求体；enabled 未传视为启用
type depreciationRuleRequest struct {
	CompanyID int64   `json:"company_id" binding:"required"`
	Name      string  `json:"name"`
	Months    int     `json:"months"`
	FloorType string  `json:"floor_type"`
	FloorVal  float64 `json:"floor_val"`
	Stages    string  `json:"stages"`
	Enabled   *bool   `json:"enabled"`
	Remark    string  `json:"remark"`
}

func (req depreciationRuleRequest) toRule(id int64) model.DepreciationRule {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	floorType := req.FloorType
	if floorType == "" {
		floorType = depreciation.FloorPercent
	}
	return model.DepreciationRule{
		BaseModel: model.BaseModel{ID: id},
		CompanyID: req.CompanyID,
		Name:      req.Name,
		Months:    req.Months,
		FloorType: floorType,
		FloorVal:  req.FloorVal,
		Stages:    req.Stages,
		Enabled:   enabled,
		Remark:    req.Remark,
	}
}

func (h *DepreciationHandler) CreateRule(c *gin.Context) {
	var req depreciationRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if err := depreciation.ValidateRuleSpec(req.Name, req.Months, req.FloorType, req.FloorVal, req.Stages); err != nil {
		Fail(c, http.StatusBadRequest, 40002, err.Error())
		return
	}
	created, err := h.store.CreateDepreciationRule(c.Request.Context(), req.toRule(0))
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "创建折旧规则失败")
		return
	}
	Success(c, created)
}

func (h *DepreciationHandler) UpdateRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	var req depreciationRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if err := depreciation.ValidateRuleSpec(req.Name, req.Months, req.FloorType, req.FloorVal, req.Stages); err != nil {
		Fail(c, http.StatusBadRequest, 40002, err.Error())
		return
	}
	updated, err := h.store.UpdateDepreciationRule(c.Request.Context(), req.toRule(id))
	if err != nil {
		failDepreciationStoreError(c, err, "更新折旧规则失败")
		return
	}
	Success(c, updated)
}

func (h *DepreciationHandler) DeleteRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	companyID, err := strconv.ParseInt(c.Query("company_id"), 10, 64)
	if err != nil || companyID <= 0 {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	// 被引用的规则不允许删除（悬空引用会让引擎冻结净值刷新，且历史口径丢失）
	var inUse int64
	if err := store.DB.Model(&model.Asset{}).
		Where("company_id = ? AND depreciation_id = ?", companyID, id).
		Count(&inUse).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询规则引用失败")
		return
	}
	if inUse > 0 {
		Fail(c, http.StatusConflict, 40901, "该折旧规则正被 "+strconv.FormatInt(inUse, 10)+" 个资产引用，请先解除挂接")
		return
	}
	if err := h.store.DeleteDepreciationRule(c.Request.Context(), companyID, id); err != nil {
		failDepreciationStoreError(c, err, "删除折旧规则失败")
		return
	}
	Success(c, gin.H{"deleted": true})
}

// Recalculate 手动触发一轮净值重算（规则/台账刚编辑完立即生效，无需等定时任务）
func (h *DepreciationHandler) Recalculate(c *gin.Context) {
	n, err := h.engine.ScanOnce(c.Request.Context())
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "净值重算失败")
		return
	}
	Success(c, gin.H{"updated": n})
}

func failDepreciationStoreError(c *gin.Context, err error, fallbackMsg string) {
	if errors.Is(err, store.ErrNotFound) {
		Fail(c, http.StatusNotFound, 40401, "折旧规则不存在")
		return
	}
	Fail(c, http.StatusInternalServerError, 50001, fallbackMsg)
}

type offBookRequest struct {
	CompanyID int64      `json:"company_id" binding:"required"`
	OffBookAt *time.Time `json:"off_book_at"` // 缺省取当前时间
}

// OffBook 财务销账转列管：折旧完且财务销账的资产继续给员工使用，
// 台账保持跟踪直到走完报废流程变卖。运营状态不动，只翻财务维度标志
func (h *DepreciationHandler) OffBook(c *gin.Context) {
	assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid asset id")
		return
	}
	var req offBookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	var asset model.Asset
	if err := store.DB.Where("id = ? AND company_id = ?", assetID, req.CompanyID).First(&asset).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "资产不存在或不属于该公司")
		return
	}
	if asset.OffBook {
		Fail(c, http.StatusConflict, 40902, "该资产已在列管（已销账）")
		return
	}
	if asset.Status == model.AssetStatusScrapped {
		Fail(c, http.StatusBadRequest, 40003, "已报废资产无需销账，请直接走变卖处置")
		return
	}

	offBookAt := time.Now().UTC()
	if req.OffBookAt != nil {
		offBookAt = *req.OffBookAt
	}
	updates := map[string]interface{}{"off_book": true, "off_book_at": offBookAt}
	if err := store.DB.Model(&asset).Updates(updates).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "销账失败")
		return
	}
	recordFinanceEvent(c, asset.ID, model.AssetEventOffBook,
		"财务销账转列管", "资产已折旧完并销财务账，转列管继续跟踪使用；销账日期："+
			offBookAt.Format("2006-01-02"), asset.NetValue)
	if err := store.DB.First(&asset, assetID).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "reload asset failed")
		return
	}
	Success(c, asset)
}

func (h *DepreciationHandler) RestoreBook(c *gin.Context) {
	assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid asset id")
		return
	}
	var req offBookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	var asset model.Asset
	if err := store.DB.Where("id = ? AND company_id = ?", assetID, req.CompanyID).First(&asset).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "资产不存在或不属于该公司")
		return
	}
	if !asset.OffBook {
		Fail(c, http.StatusConflict, 40902, "该资产未销账，无需恢复在册")
		return
	}

	updates := map[string]interface{}{"off_book": false, "off_book_at": nil}
	if err := store.DB.Model(&asset).Updates(updates).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "恢复在册失败")
		return
	}
	prevOffBookAt := "未知"
	if asset.OffBookAt != nil {
		prevOffBookAt = asset.OffBookAt.Format("2006-01-02")
	}
	recordFinanceEvent(c, asset.ID, model.AssetEventOffBookRestore,
		"恢复在册", "撤销财务销账，资产恢复在册管理；原销账日期："+prevOffBookAt, asset.NetValue)
	if err := store.DB.First(&asset, assetID).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "reload asset failed")
		return
	}
	Success(c, asset)
}

// recordFinanceEvent 财务动作在资产履历留痕（发生时净值快照入 NetValue 字段）；
// 履历写入失败不回滚主流程，错误经 gin 错误链记录（与外派登记同策略）
func recordFinanceEvent(c *gin.Context, assetID int64, eventType, title, description string, netValue float64) {
	event := model.AssetEvent{
		AssetID:     assetID,
		EventType:   eventType,
		Title:       title,
		Description: description,
		NetValue:    netValue,
		OperatorID:  currentUserID(c),
	}
	if err := store.DB.Create(&event).Error; err != nil {
		c.Error(err)
	}
}
