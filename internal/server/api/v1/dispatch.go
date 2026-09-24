package v1

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// DispatchHandler 外派登记（阶段五 A1）：长期出差/涉密客户现场终端的
// 登记、归还与作废；创建/归还动作联动 AssetEvent 履历留痕
type DispatchHandler struct {
	store *store.GormStore
}

// RegisterDispatchRoutes 注册外派管理路由：资产管理动作，仅管理员与超管可操作
func RegisterDispatchRoutes(protected *gin.RouterGroup) {
	adminOnly := protected.Group("/dispatches")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	h := &DispatchHandler{store: store.NewGormStore(store.DB)}
	{
		adminOnly.GET("", h.List)
		adminOnly.POST("", h.Create)
		adminOnly.POST("/:id/return", h.Return)
		adminOnly.POST("/:id/cancel", h.Cancel)
	}
}

type listDispatchQuery struct {
	CompanyID int64 `form:"company_id"`
	AssetID   int64 `form:"asset_id"`
	Status    int   `form:"status"`
	Overdue   *bool `form:"overdue"`
	Page      int   `form:"page,default=1"`
	PageSize  int   `form:"page_size,default=20"`
}

func (h *DispatchHandler) List(c *gin.Context) {
	var q listDispatchQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	items, total, err := h.store.ListDispatches(c.Request.Context(), store.DispatchListFilter{
		CompanyID: q.CompanyID,
		AssetID:   q.AssetID,
		Status:    q.Status,
		Overdue:   q.Overdue,
		Page:      q.Page,
		PageSize:  q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询外派记录失败")
		return
	}
	Success(c, PageResult{Total: total, Items: items})
}

type createDispatchRequest struct {
	CompanyID        int64      `json:"company_id" binding:"required"`
	AssetID          int64      `json:"asset_id" binding:"required"`
	BorrowerName     string     `json:"borrower_name" binding:"required"`
	Destination      string     `json:"destination" binding:"required"`
	DispatchedAt     *time.Time `json:"dispatched_at"`
	ExpectedReturnAt time.Time  `json:"expected_return_at" binding:"required"`
	IsolationOffline bool       `json:"isolation_offline"`
	ExpectWipe       bool       `json:"expect_wipe"`
	Remark           string     `json:"remark"`
}

func (h *DispatchHandler) Create(c *gin.Context) {
	var req createDispatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	// 公司边界校验：登记的资产必须属于该公司，防止跨租户登记
	var asset model.Asset
	if err := store.DB.Where("id = ? AND company_id = ?", req.AssetID, req.CompanyID).First(&asset).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "资产不存在或不属于该公司")
		return
	}

	d := model.AssetDispatch{
		CompanyID:        req.CompanyID,
		AssetID:          req.AssetID,
		BorrowerName:     req.BorrowerName,
		Destination:      req.Destination,
		ExpectedReturnAt: req.ExpectedReturnAt,
		IsolationOffline: req.IsolationOffline,
		ExpectWipe:       req.ExpectWipe,
		Remark:           req.Remark,
	}
	if req.DispatchedAt != nil {
		d.DispatchedAt = *req.DispatchedAt
	}
	created, err := h.store.CreateDispatch(c.Request.Context(), d)
	if err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			Fail(c, http.StatusConflict, 40901, "该资产已有外派中的记录")
			return
		}
		Fail(c, http.StatusInternalServerError, 50002, "登记外派失败")
		return
	}

	expected := created.ExpectedReturnAt.Format("2006-01-02")
	recordDispatchEvent(c, created, model.AssetEventDispatch,
		"外派登记："+created.Destination+"，预计归期 "+expected,
		"外派负责人："+created.BorrowerName+
			"；隔离现场："+boolText(created.IsolationOffline, "是", "否")+
			"；预期格式化归还："+boolText(created.ExpectWipe, "是", "否"),
		&created.ExpectedReturnAt)
	Success(c, created)
}

type returnDispatchRequest struct {
	CompanyID  int64      `json:"company_id" binding:"required"`
	ReturnedAt *time.Time `json:"returned_at"`
}

func (h *DispatchHandler) Return(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	var req returnDispatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	returnedAt := time.Now().UTC()
	if req.ReturnedAt != nil {
		returnedAt = *req.ReturnedAt
	}
	d, err := h.store.ReturnDispatch(c.Request.Context(), req.CompanyID, id, returnedAt)
	if err != nil {
		failDispatchStoreError(c, err, "归还外派失败")
		return
	}

	returnedAtCopy := returnedAt
	recordDispatchEvent(c, d, model.AssetEventDispatchReturn,
		"外派归还："+d.Destination,
		"外派负责人："+d.BorrowerName+"，实际归还时间："+returnedAt.Format("2006-01-02 15:04"),
		&returnedAtCopy)
	Success(c, d)
}

type cancelDispatchRequest struct {
	CompanyID int64 `json:"company_id" binding:"required"`
}

func (h *DispatchHandler) Cancel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	var req cancelDispatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	d, err := h.store.CancelDispatch(c.Request.Context(), req.CompanyID, id)
	if err != nil {
		failDispatchStoreError(c, err, "作废外派记录失败")
		return
	}
	Success(c, d)
}

// failDispatchStoreError 将 store 业务哨兵映射为标准错误信封：
// 不存在或跨公司→404；状态不允许流转→400；其余→500
func failDispatchStoreError(c *gin.Context, err error, fallbackMsg string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		Fail(c, http.StatusNotFound, 40401, "外派记录不存在")
	case errors.Is(err, store.ErrInvalidState):
		Fail(c, http.StatusBadRequest, 40003, "外派记录当前状态不允许该操作")
	default:
		Fail(c, http.StatusInternalServerError, 50001, fallbackMsg)
	}
}

// recordDispatchEvent 外派动作在资产履历（AssetEvent）留痕；履历写入失败
// 不回滚主流程（登记/归还已生效，履历可事后补录），错误经 gin 错误链记录
func recordDispatchEvent(c *gin.Context, d model.AssetDispatch, eventType, title, description string, returnDate *time.Time) {
	event := model.AssetEvent{
		AssetID:      d.AssetID,
		EventType:    eventType,
		Title:        title,
		Description:  description,
		TargetPerson: d.BorrowerName,
		ReturnDate:   returnDate,
		OperatorID:   currentUserID(c),
	}
	if err := store.DB.Create(&event).Error; err != nil {
		c.Error(err)
	}
}

func currentUserID(c *gin.Context) *int64 {
	v, ok := c.Get("userID")
	if !ok {
		return nil
	}
	id, ok := v.(int64)
	if !ok {
		return nil
	}
	return &id
}

func boolText(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}
