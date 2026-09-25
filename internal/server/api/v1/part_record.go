package v1

import (
	"net/http"
	"time"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"github.com/gin-gonic/gin"
)

type PartRecordHandler struct{}

// RegisterPartRecordRoutes 读面登录可读，写面（登记流水）仅 admin
//（2026-09-26 RBAC 前后端同步收口：dimension 先例）
func RegisterPartRecordRoutes(protected *gin.RouterGroup) {
	h := &PartRecordHandler{}
	read := protected.Group("/part-records")
	{
		read.GET("", h.List)
	}
	adminOnly := protected.Group("/part-records")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.POST("", h.Create)
	}
}

type ListPartRecordQuery struct {
	CompanyID int64  `form:"company_id"`
	Direction string `form:"direction"`
	PartType  string `form:"part_type"`
	AssetTag  string `form:"asset_tag"`
	Page      int    `form:"page,default=1"`
	PageSize  int    `form:"page_size,default=20"`
}

func (h *PartRecordHandler) List(c *gin.Context) {
	var query ListPartRecordQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	db := store.DB.Model(&model.PartRecord{})
	if query.CompanyID > 0 {
		db = db.Where("company_id = ?", query.CompanyID)
	}
	if query.Direction != "" {
		db = db.Where("direction = ?", query.Direction)
	}
	if query.PartType != "" {
		db = db.Where("part_type = ?", query.PartType)
	}
	if query.AssetTag != "" {
		db = db.Where("asset_tag LIKE ?", "%"+query.AssetTag+"%")
	}

	var total int64
	db.Count(&total)

	var items []model.PartRecord
	offset := (query.Page - 1) * query.PageSize
	if err := db.Preload("Operator").Offset(offset).Limit(query.PageSize).
		Order("id desc").Find(&items).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to query part records")
		return
	}

	Success(c, PageResult{
		Total: total,
		Items: items,
	})
}

type CreatePartRecordRequest struct {
	CompanyID      int64      `json:"company_id" binding:"required"`
	Direction      string     `json:"direction" binding:"required"` // in(入库) / out(出库)
	OperatedAt     *time.Time `json:"operated_at"`
	PartType       string     `json:"part_type" binding:"required"`
	PartName       string     `json:"part_name"`
	PartModel      string     `json:"part_model"`
	Brand          string     `json:"brand"`
	Quantity       int        `json:"quantity"`
	Unit           string     `json:"unit"`
	LockerLocation string     `json:"locker_location"`
	Purpose        string     `json:"purpose"`
	OANumber       string     `json:"oa_number"`
	Location       string     `json:"location"`
	AssetTag       string     `json:"asset_tag"`
}

func (h *PartRecordHandler) Create(c *gin.Context) {
	var req CreatePartRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if req.Direction != model.PartDirectionIn && req.Direction != model.PartDirectionOut {
		Fail(c, http.StatusBadRequest, 40002, "direction must be in or out")
		return
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}
	if req.OperatedAt == nil {
		now := time.Now()
		req.OperatedAt = &now
	}

	// 操作人取自 JWT 上下文，未携带时允许为空（如系统初始化导入）
	var operatorID *int64
	if v, exists := c.Get("userID"); exists {
		if uid, ok := v.(int64); ok {
			operatorID = &uid
		}
	}

	item := model.PartRecord{
		CompanyID:      req.CompanyID,
		Direction:      req.Direction,
		OperatedAt:     req.OperatedAt,
		PartType:       req.PartType,
		PartName:       req.PartName,
		PartModel:      req.PartModel,
		Brand:          req.Brand,
		Quantity:       req.Quantity,
		Unit:           req.Unit,
		LockerLocation: req.LockerLocation,
		Purpose:        req.Purpose,
		OANumber:       req.OANumber,
		Location:       req.Location,
		AssetTag:       req.AssetTag,
		OperatorID:     operatorID,
	}
	if err := store.DB.Create(&item).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "failed to create part record: "+err.Error())
		return
	}
	Success(c, item)
}
