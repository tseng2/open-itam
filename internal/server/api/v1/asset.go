package v1

import (
	"net/http"
	"strconv"
	"strings"
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
		assets.GET("/:id/events", h.ListEvents)
		assets.GET("/:id/versions", h.ListVersions)
		assets.POST("/:id/events/:event_id/approve", h.ApproveEvent)
	}
}

func (h *AssetHandler) ListEvents(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid asset id")
		return
	}
	var events []model.AssetEvent
	if err := store.DB.Preload("Operator").Where("asset_id = ?", id).Order("created_at desc").Find(&events).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to query events")
		return
	}
	Success(c, events)
}

func (h *AssetHandler) ListVersions(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid asset id")
		return
	}
	var versions []model.AssetVersion
	if err := store.DB.Preload("ApprovedBy").Where("asset_id = ?", id).Order("version desc").Find(&versions).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to query versions")
		return
	}
	Success(c, versions)
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

func (h *AssetHandler) ApproveEvent(c *gin.Context) {
	assetID, err1 := strconv.ParseUint(c.Param("id"), 10, 64)
	eventID, err2 := strconv.ParseUint(c.Param("event_id"), 10, 64)
	if err1 != nil || err2 != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}

	var req struct {
		OANumber string  `json:"oa_number"`
		Cost     float64 `json:"cost"`
	}
	_ = c.ShouldBindJSON(&req)

	var event model.AssetEvent
	if err := store.DB.First(&event, eventID).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "event not found")
		return
	}

	if event.AssetID != int64(assetID) {
		Fail(c, http.StatusBadRequest, 40003, "event does not belong to this asset")
		return
	}

	if event.ReviewStatus != 20 {
		Fail(c, http.StatusBadRequest, 40004, "event is not in pending review status")
		return
	}

	// 审批通过，更新基线
	var asset model.Asset
	if err := store.DB.First(&asset, assetID).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "asset not found")
		return
	}

	desc := event.Description
	snapIdx := strings.LastIndex(desc, "当前快照: ")
	if snapIdx == -1 {
		Fail(c, http.StatusInternalServerError, 50002, "cannot find hardware snapshot in event")
		return
	}
	hwSnapshot := desc[snapIdx+len("当前快照: "):]

	asset.CurrentVersion += 1
	store.DB.Save(&asset)

	// UserID from JWT token 
	var opID *int64
	// In a real implementation we would extract from Context, e.g.:
	// if val, exists := c.Get("userID"); exists { ... }

	newVer := model.AssetVersion{
		AssetID:          asset.ID,
		Version:          asset.CurrentVersion,
		HardwareSnapshot: hwSnapshot,
		ChangeReason:     "硬件配置变更审批通过",
		ApprovedByID:     opID,
	}
	store.DB.Create(&newVer)

	event.ReviewStatus = 10 // 已完成
	event.OANumber = req.OANumber
	event.Cost = req.Cost
	store.DB.Save(&event)

	Success(c, gin.H{"message": "approved"})
}
