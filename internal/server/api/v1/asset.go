package v1

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
		assets.DELETE("/:id", h.Delete)
		assets.POST("/:id/merge", h.Merge)
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
	Keyword   string `form:"keyword"` // 全文模糊搜索：编码/SN/MAC/IP/使用人等台账字段
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
	if query.Keyword != "" {
		// 跨台账主表、关联终端、领用人的全文模糊匹配；
		// agent_devices.asset_id 与 users.id 均为 1:1 关联，JOIN 不会放大行数
		kw := "%" + query.Keyword + "%"
		cols := []string{
			"assets.asset_tag", "assets.serial_number", "assets.mac_address",
			"assets.brand", "assets.model_name", "assets.cpu_name",
			"assets.manager_name", "assets.location", "assets.department_name",
			"assets.department_sub", "assets.u8_order_no", "assets.remark",
			"agent_devices.hostname", "agent_devices.ip_address",
			"agent_devices.public_ip", "agent_devices.mac_address",
			"users.real_name", "users.username",
		}
		conds := make([]string, len(cols))
		args := make([]interface{}, len(cols))
		for i, col := range cols {
			conds[i] = col + " LIKE ?"
			args[i] = kw
		}
		db = db.Joins("LEFT JOIN agent_devices ON agent_devices.asset_id = assets.id").
			Joins("LEFT JOIN users ON users.id = assets.user_id").
			Where(strings.Join(conds, " OR "), args...)
	}

	var total int64
	db.Count(&total)

	var items []model.Asset
	offset := (query.Page - 1) * query.PageSize
	if err := db.Preload("Company").Preload("User").Preload("Device").
		Offset(offset).Limit(query.PageSize).
		Order("assets.id desc").Find(&items).Error; err != nil {
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
	setStr("model_name", req.ModelName) // DB 列名是 model_name（GORM 对 ModelName 的默认蛇形映射），不是 JSON 里的 model
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

// Delete 软删除资产：解绑关联终端（该终端下次上报会重新生成待编资产），
// 履历/维修/基线版本记录保留，可通过合并或数据库追溯
func (h *AssetHandler) Delete(c *gin.Context) {
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

	err = store.DB.Transaction(func(tx *gorm.DB) error {
		// 终端解绑放在删除前：agent_devices.asset_id 有唯一索引，软删的资产不能继续占用
		if err := tx.Model(&model.Device{}).Where("asset_id = ?", asset.ID).
			Update("asset_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Delete(&asset).Error; err != nil {
			return err
		}
		event := model.AssetEvent{
			AssetID:   asset.ID,
			EventType: "delete",
			Title:     "资产台账删除",
			Description: "资产 " + asset.AssetTag + " 被删除（软删除，数据保留）",
		}
		return tx.Create(&event).Error
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50004, "failed to delete asset: "+err.Error())
		return
	}
	Success(c, gin.H{"message": "deleted"})
}

type MergeAssetRequest struct {
	TargetID int64 `json:"target_id" binding:"required"` // 合并后保留的资产ID（源资产数据迁入后被软删除）
}

// Merge 把源资产（:id）并入目标资产（target_id）：终端绑定、履历、维修、基线版本全部迁移，
// 目标资产的空账面字段用源资产补齐，源资产软删除
func (h *AssetHandler) Merge(c *gin.Context) {
	srcID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid asset id")
		return
	}
	var req MergeAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if req.TargetID == int64(srcID) {
		Fail(c, http.StatusBadRequest, 40003, "cannot merge asset into itself")
		return
	}

	var src, dst model.Asset
	if err := store.DB.First(&src, srcID).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "source asset not found")
		return
	}
	if err := store.DB.First(&dst, req.TargetID).Error; err != nil {
		Fail(c, http.StatusNotFound, 40402, "target asset not found")
		return
	}

	err = store.DB.Transaction(func(tx *gorm.DB) error {
		// 终端迁移：一台资产只能绑一个终端（唯一索引）。两边都有终端时
		// 保留"活的"（最近心跳新的），旧的解绑——重复登记常见于一旧一新两条待编
		var srcDev, dstDev model.Device
		srcHas := tx.Where("asset_id = ?", src.ID).First(&srcDev).Error == nil
		dstHas := tx.Where("asset_id = ?", dst.ID).First(&dstDev).Error == nil
		switch {
		case srcHas && dstHas:
			if srcDev.LastSeenAt.After(dstDev.LastSeenAt) {
				if err := tx.Model(&model.Device{}).Where("id = ?", dstDev.ID).
					Update("asset_id", nil).Error; err != nil {
					return err
				}
				if err := tx.Model(&model.Device{}).Where("id = ?", srcDev.ID).
					Update("asset_id", dst.ID).Error; err != nil {
					return err
				}
			} else if err := tx.Model(&model.Device{}).Where("id = ?", srcDev.ID).
				Update("asset_id", nil).Error; err != nil {
				return err
			}
		case srcHas:
			if err := tx.Model(&model.Device{}).Where("id = ?", srcDev.ID).
				Update("asset_id", dst.ID).Error; err != nil {
				return err
			}
		}

		if err := tx.Model(&model.AssetEvent{}).Where("asset_id = ?", src.ID).
			Update("asset_id", dst.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.AssetRepair{}).Where("asset_id = ?", src.ID).
			Update("asset_id", dst.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.AssetVersion{}).Where("asset_id = ?", src.ID).
			Update("asset_id", dst.ID).Error; err != nil {
			return err
		}

		// 目标资产的空账面字段用源资产值补齐，已有人工维护值不覆盖
		fill := map[string]interface{}{}
		if dst.SerialNumber == "" {
			fill["serial_number"] = src.SerialNumber
		}
		if dst.Brand == "" {
			fill["brand"] = src.Brand
		}
		if dst.ModelName == "" {
			fill["model_name"] = src.ModelName
		}
		if dst.CPUName == "" {
			fill["cpu_name"] = src.CPUName
		}
		if dst.MemorySize == "" {
			fill["memory_size"] = src.MemorySize
		}
		if dst.MainDisk == "" {
			fill["main_disk"] = src.MainDisk
		}
		if dst.SecondaryDisk == "" {
			fill["secondary_disk"] = src.SecondaryDisk
		}
		if dst.GPUName == "" {
			fill["gpu_name"] = src.GPUName
		}
		if dst.MACAddress == "" {
			fill["mac_address"] = src.MACAddress
		}
		if dst.PurchaseDate == nil {
			fill["purchase_date"] = src.PurchaseDate
		}
		if dst.U8OrderNo == "" {
			fill["u8_order_no"] = src.U8OrderNo
		}
		if len(fill) > 0 {
			if err := tx.Model(&dst).Updates(fill).Error; err != nil {
				return err
			}
		}

		event := model.AssetEvent{
			AssetID:   dst.ID,
			EventType: "merge",
			Title:     "合并重复资产台账",
			Description: "将资产 " + src.AssetTag + " (ID:" + strconv.FormatInt(src.ID, 10) + ") 并入本记录，源记录已删除",
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		return tx.Delete(&src).Error
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50005, "failed to merge asset: "+err.Error())
		return
	}
	Success(c, gin.H{"message": "merged", "target_id": dst.ID})
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
