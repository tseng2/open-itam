package v1

import (
	"net/http"
	"strconv"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"github.com/gin-gonic/gin"
)

type AssetHandler struct{}

func RegisterAssetRoutes(r *gin.RouterGroup) {
	h := &AssetHandler{}
	assets := r.Group("/assets")
	{
		assets.GET("", h.List)
		assets.POST("", h.Create)
		assets.GET("/:id", h.Get)
	}
}

type ListAssetQuery struct {
	CompanyID int64  `form:"company_id"`
	U8OrderNo string `form:"u8_order_no"`
	Status    int    `form:"status"`
	AssetTag  string `form:"asset_tag"`
	Page      int    `form:"page,default=1"`
	PageSize  int    `form:"page_size,default=20"`
}

func (h *AssetHandler) List(c *gin.Context) {
	var query ListAssetQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	db := store.DB.Model(&model.Asset{})

	if query.CompanyID > 0 {
		db = db.Where("company_id = ?", query.CompanyID)
	}
	if query.U8OrderNo != "" {
		db = db.Where("u8_order_no LIKE ?", "%"+query.U8OrderNo+"%")
	}
	if query.Status > 0 {
		db = db.Where("status = ?", query.Status)
	}
	if query.AssetTag != "" {
		db = db.Where("asset_tag LIKE ?", "%"+query.AssetTag+"%")
	}

	var total int64
	db.Count(&total)

	var items []model.Asset
	offset := (query.Page - 1) * query.PageSize
	if err := db.Preload("Company").Preload("User").Preload("Device").
		Offset(offset).Limit(query.PageSize).
		Order("id desc").Find(&items).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to query assets")
		return
	}

	Success(c, PageResult{
		Total: total,
		Items: items,
	})
}

type CreateAssetRequest struct {
	CompanyID    int64      `json:"company_id" binding:"required"`
	CategoryID   int64      `json:"category_id" binding:"required"`
	AssetTag     string     `json:"asset_tag" binding:"required"`
	U8OrderNo    string     `json:"u8_order_no"`
	UserID       *int64     `json:"user_id"`
	Status       int        `json:"status"`
	Brand        string     `json:"brand"`
	ModelName    string     `json:"model"`
	SerialNumber string     `json:"serial_number"`
	PurchaseDate *time.Time `json:"purchase_date"`
	Price        float64    `json:"price"`
	Remark       string     `json:"remark"`
}

func (h *AssetHandler) Create(c *gin.Context) {
	var req CreateAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	if req.Status == 0 {
		req.Status = 10 // 默认为库存中
	}

	asset := model.Asset{
		CompanyID:    req.CompanyID,
		CategoryID:   req.CategoryID,
		AssetTag:     req.AssetTag,
		U8OrderNo:    req.U8OrderNo,
		UserID:       req.UserID,
		Status:       req.Status,
		Brand:        req.Brand,
		ModelName:    req.ModelName,
		SerialNumber: req.SerialNumber,
		PurchaseDate: req.PurchaseDate,
		Price:        req.Price,
		Remark:       req.Remark,
	}

	if err := store.DB.Create(&asset).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "failed to create asset: "+err.Error())
		return
	}

	// 记录初始建账事件
	event := model.AssetEvent{
		AssetID:   asset.ID,
		EventType: "create",
		Title:     "资产建账入库",
		Description: "系统录入资产，U8订单号: " + req.U8OrderNo,
	}
	store.DB.Create(&event)

	Success(c, asset)
}

func (h *AssetHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid asset id")
		return
	}

	var asset model.Asset
	if err := store.DB.Preload("Company").Preload("User").First(&asset, id).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "asset not found")
		return
	}

	Success(c, asset)
}
