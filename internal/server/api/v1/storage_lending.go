package v1

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"github.com/gin-gonic/gin"
)

type StorageLendingHandler struct{}

// RegisterStorageLendingRoutes 读面登录可读（列表），写面仅 admin
//（2026-09-26 RBAC 前后端同步收口：dimension 先例——同前缀读/写两组注册）
func RegisterStorageLendingRoutes(protected *gin.RouterGroup) {
	h := &StorageLendingHandler{}
	read := protected.Group("/storage-lendings")
	{
		read.GET("", h.List)
	}
	adminOnly := protected.Group("/storage-lendings")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.POST("", h.Create)
		adminOnly.PUT("/:id", h.Update)
	}
}

type ListStorageLendingQuery struct {
	CompanyID  int64  `form:"company_id"`
	Department string `form:"department"`
	Borrower   string `form:"borrower"`
	Page       int    `form:"page,default=1"`
	PageSize   int    `form:"page_size,default=20"`
}

func (h *StorageLendingHandler) List(c *gin.Context) {
	var query ListStorageLendingQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	db := store.DB.Model(&model.StorageLending{})
	if query.CompanyID > 0 {
		db = db.Where("company_id = ?", query.CompanyID)
	}
	if query.Department != "" {
		db = db.Where("department LIKE ?", "%"+query.Department+"%")
	}
	if query.Borrower != "" {
		db = db.Where("borrower LIKE ?", "%"+query.Borrower+"%")
	}

	var total int64
	db.Count(&total)

	var items []model.StorageLending
	offset := (query.Page - 1) * query.PageSize
	if err := db.Preload("Company").Offset(offset).Limit(query.PageSize).
		Order("id desc").Find(&items).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to query storage lendings")
		return
	}

	Success(c, PageResult{
		Total: total,
		Items: items,
	})
}

type CreateStorageLendingRequest struct {
	CompanyID    int64      `json:"company_id" binding:"required"`
	Department   string     `json:"department"`
	Borrower     string     `json:"borrower" binding:"required"`
	BorrowDate   *time.Time `json:"borrow_date"`
	Brand        string     `json:"brand"`
	Spec         string     `json:"spec"`
	DeviceCode   string     `json:"device_code"`
	Quantity     int        `json:"quantity"`
	ReturnDate   *time.Time `json:"return_date"`
	ReturnQty    int        `json:"return_qty"`
	SecCertified bool       `json:"sec_certified"`
	Remark       string     `json:"remark"`
}

func (h *StorageLendingHandler) Create(c *gin.Context) {
	var req CreateStorageLendingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	item := model.StorageLending{
		CompanyID:    req.CompanyID,
		Department:   req.Department,
		Borrower:     req.Borrower,
		BorrowDate:   req.BorrowDate,
		Brand:        req.Brand,
		Spec:         req.Spec,
		DeviceCode:   req.DeviceCode,
		Quantity:     req.Quantity,
		ReturnDate:   req.ReturnDate,
		ReturnQty:    req.ReturnQty,
		SecCertified: req.SecCertified,
		Remark:       req.Remark,
	}
	if err := store.DB.Create(&item).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "failed to create storage lending: "+err.Error())
		return
	}
	Success(c, item)
}

// UpdateStorageLendingRequest 使用指针类型区分"未传"与"显式清空"：
// 未传=保持原值、显式 null=清空（前端编辑清日期必须带 null）、有值=更新
type UpdateStorageLendingRequest struct {
	Department   *string    `json:"department"`
	Borrower     *string    `json:"borrower"`
	BorrowDate   *time.Time `json:"borrow_date"`
	Brand        *string    `json:"brand"`
	Spec         *string    `json:"spec"`
	DeviceCode   *string    `json:"device_code"`
	Quantity     *int       `json:"quantity"`
	ReturnDate   *time.Time `json:"return_date"`
	ReturnQty    *int       `json:"return_qty"`
	SecCertified *bool      `json:"sec_certified"`
	Remark       *string    `json:"remark"`
}

// assignStorageUpdate 落实 PUT 指针字段契约：非 nil=写入新值；JSON 键存在但
// 值为显式 null=写 NULL 清空；键缺失=保持原值。指针经反序列化后无法区分
// "未传"与"显式 null"（两者都是 nil），需借助原始键集合判定
func assignStorageUpdate[T any](raw map[string]json.RawMessage, updates map[string]interface{}, key string, p *T) {
	if p != nil {
		updates[key] = *p
	} else if _, ok := raw[key]; ok {
		updates[key] = nil
	}
}

func (h *StorageLendingHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}

	var item model.StorageLending
	if err := store.DB.First(&item, id).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "storage lending not found")
		return
	}

	body, err := c.GetRawData()
	if err != nil {
		Fail(c, http.StatusBadRequest, 40001, "failed to read request body")
		return
	}
	var req UpdateStorageLendingRequest
	if err := json.Unmarshal(body, &req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	updates := map[string]interface{}{}
	assignStorageUpdate(raw, updates, "department", req.Department)
	assignStorageUpdate(raw, updates, "borrower", req.Borrower)
	assignStorageUpdate(raw, updates, "borrow_date", req.BorrowDate)
	assignStorageUpdate(raw, updates, "brand", req.Brand)
	assignStorageUpdate(raw, updates, "spec", req.Spec)
	assignStorageUpdate(raw, updates, "device_code", req.DeviceCode)
	assignStorageUpdate(raw, updates, "quantity", req.Quantity)
	assignStorageUpdate(raw, updates, "return_date", req.ReturnDate)
	assignStorageUpdate(raw, updates, "return_qty", req.ReturnQty)
	assignStorageUpdate(raw, updates, "sec_certified", req.SecCertified)
	assignStorageUpdate(raw, updates, "remark", req.Remark)

	if len(updates) > 0 {
		if err := store.DB.Model(&item).Updates(updates).Error; err != nil {
			Fail(c, http.StatusInternalServerError, 50003, "failed to update storage lending: "+err.Error())
			return
		}
	}
	if err := store.DB.Preload("Company").First(&item, id).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to reload storage lending")
		return
	}
	Success(c, item)
}
