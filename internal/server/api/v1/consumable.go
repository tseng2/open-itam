package v1

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// P2 耗材管理：库存物料 + 追加式出入库流水。读面开放所有登录用户
//（库存可见性全员，领用流程线下），写面仅 admin；库存只经
// stock-in / stock-out / adjust 三个端点变更（审计中间件自动按
// URL 尾段留痕动作）。操作人快照取自 JWT（审计口径一致）

type ConsumableHandler struct {
	store *store.GormStore
}

// RegisterConsumableRoutes 注册耗材路由：读/写两组注册（dimension 先例）
func RegisterConsumableRoutes(protected *gin.RouterGroup) {
	h := &ConsumableHandler{store: store.NewGormStore(store.DB)}

	read := protected.Group("/consumables")
	{
		read.GET("", h.List)
		read.GET("/:id/txns", h.ListTxns)
	}
	adminOnly := protected.Group("/consumables")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.POST("", h.Create)
		adminOnly.PUT("/:id", h.Update)
		adminOnly.DELETE("/:id", h.Delete)
		adminOnly.POST("/:id/stock-in", h.StockIn)
		adminOnly.POST("/:id/stock-out", h.StockOut)
		adminOnly.POST("/:id/adjust", h.Adjust)
	}
}

type consumableListQuery struct {
	CompanyID int64  `form:"company_id" binding:"required"`
	Keyword   string `form:"keyword"`
	LowStock  *bool  `form:"low_stock"` // true 仅库存预警项
	Page      int    `form:"page,default=1"`
	PageSize  int    `form:"page_size,default=50"`
}

func (h *ConsumableHandler) List(c *gin.Context) {
	var q consumableListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	items, total, err := h.store.ListConsumables(c.Request.Context(), store.ConsumableListFilter{
		CompanyID: q.CompanyID, Keyword: q.Keyword, LowStock: q.LowStock,
		Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询耗材失败")
		return
	}
	// 预警派生：配了预警线且库存触线（口径与 store 过滤一致，展示层单源）
	for i := range items {
		items[i].LowStock = items[i].MinQuantity > 0 && items[i].Stock <= items[i].MinQuantity
	}
	Success(c, PageResult{Total: total, Items: items})
}

func (h *ConsumableHandler) ListTxns(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	txnType := c.Query("type")
	if txnType != "" && !model.ConsumableTxnTypes[txnType] {
		Fail(c, http.StatusBadRequest, 40003, "流水类型不合法")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	items, total, err := h.store.ListConsumableTxns(c.Request.Context(), store.ConsumableTxnListFilter{
		CompanyID: companyID, ConsumableID: id, Type: txnType, Page: page, PageSize: pageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询耗材流水失败")
		return
	}
	Success(c, PageResult{Total: total, Items: items})
}

type consumableRequest struct {
	CompanyID   int64  `json:"company_id" binding:"required"`
	Name        string `json:"name"`
	Spec        string `json:"spec"`
	Unit        string `json:"unit"`
	MinQuantity int    `json:"min_quantity"`
	Remark      string `json:"remark"`
}

func (h *ConsumableHandler) Create(c *gin.Context) {
	var req consumableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	created, err := h.store.CreateConsumable(c.Request.Context(), model.Consumable{
		CompanyID: req.CompanyID, Name: req.Name, Spec: req.Spec,
		Unit: req.Unit, MinQuantity: req.MinQuantity, Remark: req.Remark,
	})
	if err != nil {
		failConsumableStoreError(c, err, "创建耗材失败")
		return
	}
	Success(c, created)
}

func (h *ConsumableHandler) Update(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	var req consumableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	updated, err := h.store.UpdateConsumable(c.Request.Context(), model.Consumable{
		BaseModel:  model.BaseModel{ID: id},
		CompanyID:  req.CompanyID, Name: req.Name, Spec: req.Spec,
		Unit: req.Unit, MinQuantity: req.MinQuantity, Remark: req.Remark,
	})
	if err != nil {
		failConsumableStoreError(c, err, "更新耗材失败")
		return
	}
	Success(c, updated)
}

// Delete 删除拦截：有出入库流水的耗材不允许删（流水历史会悬空），
// 409 带流水计数
func (h *ConsumableHandler) Delete(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	txnRefs, ok := countRefs(c, &model.ConsumableTxn{}, "company_id = ? AND consumable_id = ?", companyID, id)
	if !ok {
		return
	}
	if txnRefs > 0 {
		failDimensionInUse(c, txnRefs, "耗材（有出入库流水）")
		return
	}
	if err := h.store.DeleteConsumable(c.Request.Context(), companyID, id); err != nil {
		failConsumableStoreError(c, err, "删除耗材失败")
		return
	}
	Success(c, gin.H{"deleted": true})
}

// 流水请求体：stock-in/out 用正数量（服务端换算带符号增量）；
// adjust 用带符号 delta。操作人取 JWT 身份做快照
type consumableTxnRequest struct {
	CompanyID int64  `json:"company_id" binding:"required"`
	Quantity  int    `json:"quantity"` // 入库/出库数量（正数）
	Delta     int    `json:"delta"`   // 调整增量（带符号非零）
	Recipient string `json:"recipient"`
	Remark    string `json:"remark"`
}

// operatorSnapshot 从 JWT 取操作人快照（ID + 登录名）
func operatorSnapshot(c *gin.Context) (int64, string) {
	username, _ := c.Get("username")
	name, _ := username.(string)
	return currentSelfID(c), name
}

func (h *ConsumableHandler) postTxn(c *gin.Context, txnType string, delta int, req consumableTxnRequest) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	operatorID, operatorName := operatorSnapshot(c)
	txn, consumable, err := h.store.CreateConsumableTxn(c.Request.Context(), model.ConsumableTxn{
		CompanyID:    req.CompanyID,
		ConsumableID: id,
		Type:         txnType,
		Delta:        delta,
		Recipient:    req.Recipient,
		OperatorID:   operatorID,
		OperatorName: operatorName,
		Remark:       req.Remark,
	})
	if err != nil {
		failConsumableStoreError(c, err, "登记耗材流水失败")
		return
	}
	Success(c, gin.H{"txn": txn, "consumable": consumable})
}

func (h *ConsumableHandler) StockIn(c *gin.Context) {
	var req consumableTxnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if req.Quantity <= 0 {
		Fail(c, http.StatusBadRequest, 40003, "入库数量必须为正数")
		return
	}
	h.postTxn(c, model.ConsumableTxnStockIn, req.Quantity, req)
}

func (h *ConsumableHandler) StockOut(c *gin.Context) {
	var req consumableTxnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if req.Quantity <= 0 {
		Fail(c, http.StatusBadRequest, 40003, "出库数量必须为正数")
		return
	}
	// 出库落库为负增量：库存恒为「加增量」单一口径
	h.postTxn(c, model.ConsumableTxnStockOut, -req.Quantity, req)
}

func (h *ConsumableHandler) Adjust(c *gin.Context) {
	var req consumableTxnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if req.Delta == 0 {
		Fail(c, http.StatusBadRequest, 40003, "调整增量不能为零")
		return
	}
	h.postTxn(c, model.ConsumableTxnAdjust, req.Delta, req)
}

// failConsumableStoreError 耗材写操作统一错误映射：
// NotFound → 404；AlreadyExists → 409 重名；Insufficient → 409 库存不足
func failConsumableStoreError(c *gin.Context, err error, action string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		Fail(c, http.StatusNotFound, 40401, "耗材不存在")
	case errors.Is(err, store.ErrAlreadyExists):
		Fail(c, http.StatusConflict, 40901, "该公司已存在同名耗材")
	case errors.Is(err, store.ErrInsufficient):
		Fail(c, http.StatusConflict, 40903, "库存不足，出库数量不得超过当前库存")
	default:
		Fail(c, http.StatusInternalServerError, 50001, action)
	}
}
