package v1

import (
	"net/http"
	"strconv"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"github.com/gin-gonic/gin"
)

type AssetRepairHandler struct{}

func RegisterAssetRepairRoutes(r *gin.RouterGroup) {
	h := &AssetRepairHandler{}
	assets := r.Group("/assets/:id/repairs")
	{
		assets.GET("", h.List)
		assets.POST("", h.Create)
		assets.PUT("/:repair_id", h.Update)
	}
}

func (h *AssetRepairHandler) List(c *gin.Context) {
	assetID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid asset id")
		return
	}
	var repairs []model.AssetRepair
	if err := store.DB.Where("asset_id = ?", assetID).Order("id desc").Find(&repairs).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to query repairs")
		return
	}
	Success(c, repairs)
}

type CreateAssetRepairRequest struct {
	OANumber     string     `json:"oa_number"`
	UserName     string     `json:"user_name"`
	FaultReason  string     `json:"fault_reason"`
	Diagnosis    string     `json:"diagnosis"`
	Suggestion   string     `json:"suggestion"`
	Vendor       string     `json:"vendor"`
	ContactName  string     `json:"contact_name"`
	ContactPhone string     `json:"contact_phone"`
	SendDate     *time.Time `json:"send_date"`
	Cost         float64    `json:"cost"`
}

func (h *AssetRepairHandler) Create(c *gin.Context) {
	assetID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid asset id")
		return
	}
	var asset model.Asset
	if err := store.DB.First(&asset, assetID).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "asset not found")
		return
	}

	var req CreateAssetRepairRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	repair := model.AssetRepair{
		AssetID:      asset.ID,
		OANumber:     req.OANumber,
		UserName:     req.UserName,
		FaultReason:  req.FaultReason,
		Diagnosis:    req.Diagnosis,
		Suggestion:   req.Suggestion,
		Vendor:       req.Vendor,
		ContactName:  req.ContactName,
		ContactPhone: req.ContactPhone,
		SendDate:     req.SendDate,
		Cost:         req.Cost,
		Status:       "repairing",
	}
	if err := store.DB.Create(&repair).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "failed to create repair: "+err.Error())
		return
	}

	// 外寄送修期间资产置为维修中，履历留痕
	if asset.Status != 40 { // 已报废资产不再变更状态
		store.DB.Model(&asset).Update("status", 30)
	}
	event := model.AssetEvent{
		AssetID:     asset.ID,
		EventType:   "repair",
		Title:       "外寄维修送修登记",
		Description: "寄修厂商: " + req.Vendor + "，故障原因: " + req.FaultReason,
		OANumber:    req.OANumber,
		Cost:        req.Cost,
	}
	store.DB.Create(&event)

	Success(c, repair)
}

// UpdateAssetRepairRequest 使用指针类型区分"未传"与"显式清空"
type UpdateAssetRepairRequest struct {
	OANumber     *string    `json:"oa_number"`
	UserName     *string    `json:"user_name"`
	FaultReason  *string    `json:"fault_reason"`
	Diagnosis    *string    `json:"diagnosis"`
	Suggestion   *string    `json:"suggestion"`
	Vendor       *string    `json:"vendor"`
	ContactName  *string    `json:"contact_name"`
	ContactPhone *string    `json:"contact_phone"`
	SendDate     *time.Time `json:"send_date"`
	ReturnDate   *time.Time `json:"return_date"`
	Cost         *float64   `json:"cost"`
	Result       *string    `json:"result"`
	Status       *string    `json:"status"`
}

func (h *AssetRepairHandler) Update(c *gin.Context) {
	assetID, err1 := strconv.ParseUint(c.Param("id"), 10, 64)
	repairID, err2 := strconv.ParseUint(c.Param("repair_id"), 10, 64)
	if err1 != nil || err2 != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}

	var repair model.AssetRepair
	if err := store.DB.First(&repair, repairID).Error; err != nil || repair.AssetID != int64(assetID) {
		Fail(c, http.StatusNotFound, 40401, "repair not found")
		return
	}

	var req UpdateAssetRepairRequest
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
	setStr("oa_number", req.OANumber)
	setStr("user_name", req.UserName)
	setStr("fault_reason", req.FaultReason)
	setStr("diagnosis", req.Diagnosis)
	setStr("suggestion", req.Suggestion)
	setStr("vendor", req.Vendor)
	setStr("contact_name", req.ContactName)
	setStr("contact_phone", req.ContactPhone)
	setStr("result", req.Result)
	setStr("status", req.Status)
	if req.SendDate != nil {
		updates["send_date"] = *req.SendDate
	}
	if req.ReturnDate != nil {
		updates["return_date"] = *req.ReturnDate
	}
	if req.Cost != nil {
		updates["cost"] = *req.Cost
	}

	if len(updates) > 0 {
		if err := store.DB.Model(&repair).Updates(updates).Error; err != nil {
			Fail(c, http.StatusInternalServerError, 50003, "failed to update repair: "+err.Error())
			return
		}
	}

	if err := store.DB.First(&repair, repairID).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to reload repair")
		return
	}

	// 设备寄回且维修结束时，资产恢复为使用中，履历留痕
	if req.Status != nil && *req.Status == "returned" {
		var asset model.Asset
		if err := store.DB.First(&asset, assetID).Error; err == nil && asset.Status == 30 {
			store.DB.Model(&asset).Update("status", 20)
		}
		event := model.AssetEvent{
			AssetID:     repair.AssetID,
			EventType:   "repair",
			Title:       "外寄维修完成寄回",
			Description: "维修结果: " + repair.Result,
			OANumber:    repair.OANumber,
			Cost:        repair.Cost,
			ReturnDate:  repair.ReturnDate,
		}
		store.DB.Create(&event)
	}

	Success(c, repair)
}
