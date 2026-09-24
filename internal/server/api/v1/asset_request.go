package v1

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// errAssetUnavailable 审批时目标资产已不在库存或已被领用：
// 与"申请不存在/状态不允许"区分，前端提示竞争失败而非操作错误
var errAssetUnavailable = errors.New("asset unavailable for assignment")

// AssetRequestHandler 设备申请（阶段五 P0-β）：普通用户提交/撤回自己的申请，
// 管理员审批。审批通过的「申请流转 + 资产绑定 + 台账状态 + 领用履历」
// 四步在同一个 GORM 事务内完成（CIYO「审批+调拨同事务」，无中间态悬挂）
type AssetRequestHandler struct {
	store *store.GormStore
}

// RegisterAssetRequestRoutes 注册设备申请路由：
// 提交/查询/撤回对登录用户开放（user 视角自动限定为自己的申请），
// 审批/驳回仅 admin 与超管
func RegisterAssetRequestRoutes(protected *gin.RouterGroup) {
	g := protected.Group("/asset-requests")
	h := &AssetRequestHandler{store: store.NewGormStore(store.DB)}
	{
		g.GET("", h.List)
		g.POST("", h.Create)
		g.GET("/:id", h.Get)
		g.POST("/:id/cancel", h.Cancel)
	}
	adminOnly := protected.Group("/asset-requests")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.POST("/:id/approve", h.Approve)
		adminOnly.POST("/:id/reject", h.Reject)
	}
}

// currentRole / currentSelfID 从 JWT claims 取角色与本人 ID
func currentRole(c *gin.Context) string {
	v, _ := c.Get("role")
	role, _ := v.(string)
	return role
}

func currentSelfID(c *gin.Context) int64 {
	v, _ := c.Get("userID")
	id, _ := v.(int64)
	return id
}

func isPrivileged(c *gin.Context) bool {
	r := currentRole(c)
	return r == "admin" || r == "super_admin"
}

type listAssetRequestQuery struct {
	CompanyID   int64 `form:"company_id" binding:"required"`
	Status     int   `form:"status"`
	ApplicantID int64 `form:"applicant_id"`
	AssetID     int64 `form:"asset_id"`
	Page       int   `form:"page,default=1"`
	PageSize   int   `form:"page_size,default=20"`
}

func (h *AssetRequestHandler) List(c *gin.Context) {
	var q listAssetRequestQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	// 越权收口：普通用户无论传什么都只看自己的申请，管理员看全队列
	if !isPrivileged(c) {
		q.ApplicantID = currentSelfID(c)
	}
	items, total, err := h.store.ListAssetRequests(c.Request.Context(), store.AssetRequestListFilter{
		CompanyID:   q.CompanyID,
		Status:      q.Status,
		ApplicantID: q.ApplicantID,
		AssetID:     q.AssetID,
		Page:        q.Page,
		PageSize:    q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询设备申请失败")
		return
	}
	Success(c, PageResult{Total: total, Items: items})
}

type createAssetRequestReq struct {
	CompanyID        int64      `json:"company_id" binding:"required"`
	AssetID          int64      `json:"asset_id" binding:"required"`
	ApplicantID      int64      `json:"applicant_id"` // 管理员代录时指定；缺省 = 当前登录人
	IsLongTerm       bool       `json:"is_long_term"`
	ExpectedReturnAt *time.Time `json:"expected_return_at"`
	Reason           string     `json:"reason" binding:"required"`
}

func (h *AssetRequestHandler) Create(c *gin.Context) {
	var req createAssetRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	// 申请人解析：管理员可代员工录入，其余人一律只能以本人名义申请
	applicantID := currentSelfID(c)
	if isPrivileged(c) && req.ApplicantID > 0 {
		applicantID = req.ApplicantID
	}
	if applicantID <= 0 {
		Fail(c, http.StatusBadRequest, 40002, "无法识别申请人")
		return
	}
	var applicant model.User
	if err := store.DB.WithContext(c.Request.Context()).
		Where("id = ? AND company_id = ?", applicantID, req.CompanyID).
		First(&applicant).Error; err != nil {
		Fail(c, http.StatusBadRequest, 40003, "申请人不存在或不属于该公司")
		return
	}
	applicantName := applicant.RealName
	if applicantName == "" {
		applicantName = applicant.Username
	}

	// 前置校验（宽松提示）：仅库存中且未被领用的资产可申请；
	// 权威校验在审批的事务内（并发下申请阶段通过不代表审批时仍可用）
	var asset model.Asset
	if err := store.DB.WithContext(c.Request.Context()).
		Where("id = ? AND company_id = ?", req.AssetID, req.CompanyID).
		First(&asset).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "资产不存在或不属于该公司")
		return
	}
	if asset.Status != model.AssetStatusStock || asset.UserID != nil {
		Fail(c, http.StatusBadRequest, 40004, "该资产当前不可申请（仅库存中且未被领用的资产可申请）")
		return
	}

	created, err := h.store.CreateAssetRequest(c.Request.Context(), model.AssetRequest{
		CompanyID:        req.CompanyID,
		AssetID:           req.AssetID,
		ApplicantID:       applicantID,
		ApplicantName:     applicantName,
		IsLongTerm:        req.IsLongTerm,
		ExpectedReturnAt:  req.ExpectedReturnAt,
		Reason:            req.Reason,
	})
	if err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			Fail(c, http.StatusConflict, 40901, "你已有该资产的待审批申请")
			return
		}
		Fail(c, http.StatusInternalServerError, 50002, "提交申请失败")
		return
	}
	Success(c, created)
}

type assetRequestActionReq struct {
	CompanyID      int64  `json:"company_id" binding:"required"`
	DecisionRemark string `json:"decision_remark"`
}

func (h *AssetRequestHandler) Get(c *gin.Context) {
	id, req, ok := parseAssetRequestAction(c, false)
	if !ok {
		return
	}
	r, err := h.store.GetAssetRequest(c.Request.Context(), req.CompanyID, id)
	if err != nil {
		failAssetRequestStoreError(c, err)
		return
	}
	// 越权收口：普通用户只能看自己的申请（跨人按不存在处理）
	if !isPrivileged(c) && r.ApplicantID != currentSelfID(c) {
		Fail(c, http.StatusNotFound, 40401, "设备申请不存在")
		return
	}
	Success(c, r)
}

func (h *AssetRequestHandler) Cancel(c *gin.Context) {
	id, req, ok := parseAssetRequestAction(c, true)
	if !ok {
		return
	}
	// 撤回权限：申请人本人或管理员；跨人跨公司统一按不存在处理
	if !isPrivileged(c) {
		r, err := h.store.GetAssetRequest(c.Request.Context(), req.CompanyID, id)
		if err != nil {
			failAssetRequestStoreError(c, err)
			return
		}
		if r.ApplicantID != currentSelfID(c) {
			Fail(c, http.StatusNotFound, 40401, "设备申请不存在")
			return
		}
	}
	r, err := h.store.CancelAssetRequest(c.Request.Context(), req.CompanyID, id)
	if err != nil {
		failAssetRequestStoreError(c, err)
		return
	}
	Success(c, r)
}

func (h *AssetRequestHandler) Reject(c *gin.Context) {
	id, req, ok := parseAssetRequestAction(c, true)
	if !ok {
		return
	}
	r, err := h.store.RejectAssetRequest(c.Request.Context(), req.CompanyID, id, currentSelfID(c), req.DecisionRemark, time.Now().UTC())
	if err != nil {
		failAssetRequestStoreError(c, err)
		return
	}
	Success(c, r)
}

// Approve 审批通过：CIYO「审批+调拨同事务」——申请 pending→approved、
// 资产绑定领用人（库存中且未被领用才命中，竞争申请仅一个能赢，失败方
// 保持 pending 等待管理员处理）、台账状态 10→20、领用履历四步一个事务
func (h *AssetRequestHandler) Approve(c *gin.Context) {
	id, req, ok := parseAssetRequestAction(c, true)
	if !ok {
		return
	}
	r, err := h.store.GetAssetRequest(c.Request.Context(), req.CompanyID, id)
	if err != nil {
		failAssetRequestStoreError(c, err)
		return
	}

	now := time.Now().UTC()
	approverID := currentSelfID(c)
	err = store.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.AssetRequest{}).
			Where("id = ? AND company_id = ? AND status = ?", id, req.CompanyID, model.AssetRequestStatusPending).
			Updates(map[string]any{
				"status":          model.AssetRequestStatusApproved,
				"approved_by":    approverID,
				"approved_at":     now,
				"decision_remark": req.DecisionRemark,
				"updated_at":      now,
			})
		if res.Error != nil {
			return fmt.Errorf("approve asset request: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return store.ErrInvalidState
		}

		// 权威绑定条件：库存中且未被领用；被抢先用不上则整单回滚（申请回到 pending）
		bind := tx.Model(&model.Asset{}).
			Where("id = ? AND company_id = ? AND status = ? AND user_id IS NULL",
				r.AssetID, req.CompanyID, model.AssetStatusStock).
			Updates(map[string]any{
				"user_id":    r.ApplicantID,
				"status":     model.AssetStatusInUse,
				"updated_at": now,
			})
		if bind.Error != nil {
			return fmt.Errorf("bind asset to applicant: %w", bind.Error)
		}
		if bind.RowsAffected == 0 {
			return errAssetUnavailable
		}

		event := model.AssetEvent{
			AssetID:      r.AssetID,
			EventType:    model.AssetEventAssign,
			Title:        assetRequestEventTitle(r),
			Description:  "申请事由：" + r.Reason + "；审批人批注：" + emptyAs(req.DecisionRemark, "无"),
			TargetPerson: r.ApplicantName,
			ReturnDate:   r.ExpectedReturnAt,
			ReviewStatus: model.AssetEventReviewDone,
			OperatorID:   &approverID,
		}
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("record assign event: %w", err)
		}
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, errAssetUnavailable):
			Fail(c, http.StatusConflict, 40902, "该资产已不在库存或已被他人领用，无法通过此申请")
		case errors.Is(err, store.ErrInvalidState):
			Fail(c, http.StatusBadRequest, 40003, "该申请当前状态不允许审批")
		default:
			Fail(c, http.StatusInternalServerError, 50002, "审批失败")
		}
		return
	}

	updated, err := h.store.GetAssetRequest(c.Request.Context(), req.CompanyID, id)
	if err != nil {
		failAssetRequestStoreError(c, err)
		return
	}
	Success(c, updated)
}

func assetRequestEventTitle(r model.AssetRequest) string {
	if r.IsLongTerm {
		return "领用审批通过（长期）"
	}
	if r.ExpectedReturnAt != nil {
		return "领用审批通过（短期借用，预计归还 " + r.ExpectedReturnAt.Format("2006-01-02") + "）"
	}
	return "领用审批通过"
}

func emptyAs(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// parseAssetRequestAction 解析路径 id 与请求体 company_id；
// withBody=false 用于 GET（无请求体，只取 company_id query）
func parseAssetRequestAction(c *gin.Context, withBody bool) (id int64, req assetRequestActionReq, ok bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return 0, req, false
	}
	if withBody {
		if err := c.ShouldBindJSON(&req); err != nil {
			Fail(c, http.StatusBadRequest, 40001, err.Error())
			return 0, req, false
		}
		return id, req, true
	}
	companyID, err := strconv.ParseInt(c.Query("company_id"), 10, 64)
	if err != nil || companyID <= 0 {
		Fail(c, http.StatusBadRequest, 40001, "company_id required")
		return 0, req, false
	}
	req.CompanyID = companyID
	return id, req, true
}

// failAssetRequestStoreError store 业务哨兵映射为标准错误信封
func failAssetRequestStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		Fail(c, http.StatusNotFound, 40401, "设备申请不存在")
	case errors.Is(err, store.ErrInvalidState):
		Fail(c, http.StatusBadRequest, 40003, "该申请当前状态不允许该操作")
	default:
		Fail(c, http.StatusInternalServerError, 50001, "操作失败")
	}
}
