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
		assets.PUT("/:id", h.Update)
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
	CompanyID      int64      `json:"company_id" binding:"required"`
	CenterName     string     `json:"center_name"`
	DepartmentName string     `json:"department_name"`
	DepartmentSub  string     `json:"department_sub"`
	Location       string     `json:"location"`
	ManagerName    string     `json:"manager_name"`
	CategoryID     int64      `json:"category_id" binding:"required"`
	CategoryName   string     `json:"category_name"`
	AssetTag       string     `json:"asset_tag" binding:"required"`
	U8OrderNo      string     `json:"u8_order_no"`
	UserID         *int64     `json:"user_id"`
	Status         int        `json:"status"`
	Brand          string     `json:"brand"`
	ModelName      string     `json:"model"`
	SerialNumber   string     `json:"serial_number"`
	CPUName        string     `json:"cpu_name"`
	MemorySize     string     `json:"memory_size"`
	MainDisk       string     `json:"main_disk"`
	SecondaryDisk  string     `json:"secondary_disk"`
	GPUName        string     `json:"gpu_name"`
	MACAddress     string     `json:"mac_address"`
	PurchaseDate   *time.Time `json:"purchase_date"`
	Acceptor       string     `json:"acceptor"`
	WarrantyPeriod string     `json:"warranty_period"`
	OriginalPrice  float64    `json:"original_price"`
	Price          float64    `json:"price"` // 兼容旧参数名
	NetValue       float64    `json:"net_value"`
	SecEncrypted   bool       `json:"sec_encrypted"`
	Remark         string     `json:"remark"`
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
	price := req.OriginalPrice
	if price == 0 && req.Price > 0 {
		price = req.Price
	}

	asset := model.Asset{
		CompanyID:      req.CompanyID,
		CenterName:     req.CenterName,
		DepartmentName: req.DepartmentName,
		DepartmentSub:  req.DepartmentSub,
		Location:       req.Location,
		ManagerName:    req.ManagerName,
		CategoryID:     req.CategoryID,
		CategoryName:   req.CategoryName,
		AssetTag:       req.AssetTag,
		U8OrderNo:      req.U8OrderNo,
		UserID:         req.UserID,
		Status:         req.Status,
		Brand:          req.Brand,
		ModelName:      req.ModelName,
		SerialNumber:   req.SerialNumber,
		CPUName:        req.CPUName,
		MemorySize:     req.MemorySize,
		MainDisk:       req.MainDisk,
		SecondaryDisk:  req.SecondaryDisk,
		GPUName:        req.GPUName,
		MACAddress:     req.MACAddress,
		PurchaseDate:   req.PurchaseDate,
		Acceptor:       req.Acceptor,
		WarrantyPeriod: req.WarrantyPeriod,
		OriginalPrice:  price,
		NetValue:       req.NetValue,
		SecEncrypted:   req.SecEncrypted,
		Remark:         req.Remark,
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

// UpdateAssetRequest 台账字段维护请求。使用指针类型以区分"未传"与"显式清空"，
// 保证台账编辑时可以把字段改回空值
type UpdateAssetRequest struct {
	CompanyID      *int64     `json:"company_id"`
	CenterName     *string    `json:"center_name"`
	DepartmentName *string    `json:"department_name"`
	DepartmentSub  *string    `json:"department_sub"`
	Location       *string    `json:"location"`
	ManagerName    *string    `json:"manager_name"`
	CategoryID     *int64     `json:"category_id"`
	CategoryName   *string    `json:"category_name"`
	AssetTag       *string    `json:"asset_tag"`
	U8OrderNo      *string    `json:"u8_order_no"`
	UserID         *int64     `json:"user_id"`
	Status         *int       `json:"status"`
	Brand          *string    `json:"brand"`
	ModelName      *string    `json:"model"`
	SerialNumber   *string    `json:"serial_number"`
	CPUName        *string    `json:"cpu_name"`
	MemorySize     *string    `json:"memory_size"`
	MainDisk       *string    `json:"main_disk"`
	SecondaryDisk  *string    `json:"secondary_disk"`
	GPUName        *string    `json:"gpu_name"`
	MACAddress     *string    `json:"mac_address"`
	PurchaseDate   *time.Time `json:"purchase_date"`
	Acceptor       *string    `json:"acceptor"`
	WarrantyPeriod *string    `json:"warranty_period"`
	OriginalPrice  *float64   `json:"original_price"`
	NetValue       *float64   `json:"net_value"`
	SecEncrypted   *bool      `json:"sec_encrypted"`
	Remark         *string    `json:"remark"`
}

func (h *AssetHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid asset id")
		return
	}

	var asset model.Asset
	if err := store.DB.First(&asset, id).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "asset not found")
		return
	}

	var req UpdateAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	updates := map[string]interface{}{}
	setStr := func(col string, v *string) {
		if v != nil {
			updates[col] = *v
		}
	}
	setStr("center_name", req.CenterName)
	setStr("department_name", req.DepartmentName)
	setStr("department_sub", req.DepartmentSub)
	setStr("location", req.Location)
	setStr("manager_name", req.ManagerName)
	setStr("category_name", req.CategoryName)
	setStr("asset_tag", req.AssetTag)
	setStr("u8_order_no", req.U8OrderNo)
	setStr("brand", req.Brand)
	setStr("model", req.ModelName)
	setStr("serial_number", req.SerialNumber)
	setStr("cpu_name", req.CPUName)
	setStr("memory_size", req.MemorySize)
	setStr("main_disk", req.MainDisk)
	setStr("secondary_disk", req.SecondaryDisk)
	setStr("gpu_name", req.GPUName)
	setStr("mac_address", req.MACAddress)
	setStr("acceptor", req.Acceptor)
	setStr("warranty_period", req.WarrantyPeriod)
	setStr("remark", req.Remark)
	if req.CompanyID != nil {
		updates["company_id"] = *req.CompanyID
	}
	if req.CategoryID != nil {
		updates["category_id"] = *req.CategoryID
	}
	if req.UserID != nil {
		if *req.UserID > 0 {
			updates["user_id"] = *req.UserID
		} else {
			updates["user_id"] = nil // 0 表示归还入库，解除领用绑定
		}
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.PurchaseDate != nil {
		updates["purchase_date"] = *req.PurchaseDate
	}
	if req.OriginalPrice != nil {
		updates["original_price"] = *req.OriginalPrice
	}
	if req.NetValue != nil {
		updates["net_value"] = *req.NetValue
	}
	if req.SecEncrypted != nil {
		updates["sec_encrypted"] = *req.SecEncrypted
	}

	if len(updates) == 0 {
		Success(c, asset)
		return
	}
	if err := store.DB.Model(&asset).Updates(updates).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50003, "failed to update asset: "+err.Error())
		return
	}

	if err := store.DB.Preload("Company").Preload("User").First(&asset, id).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to reload asset")
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
