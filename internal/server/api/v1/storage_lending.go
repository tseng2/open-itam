package v1

import (
	"net/http"
	"strconv"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"github.com/gin-gonic/gin"
)

type StorageLendingHandler struct{}

func RegisterStorageLendingRoutes(r *gin.RouterGroup) {
	h := &StorageLendingHandler{}
	g := r.Group("/storage-lendings")
	{
		g.GET("", h.List)
		g.POST("", h.Create)
		g.PUT("/:id", h.Update)
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

// UpdateStorageLendingRequest 使用指针类型区分"未传"与"显式清空"
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

	var req UpdateStorageLendingRequest
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
	setStr("department", req.Department)
	setStr("borrower", req.Borrower)
	setStr("brand", req.Brand)
	setStr("spec", req.Spec)
	setStr("device_code", req.DeviceCode)
	setStr("remark", req.Remark)
	if req.BorrowDate != nil {
		updates["borrow_date"] = *req.BorrowDate
	}
	if req.Quantity != nil {
		updates["quantity"] = *req.Quantity
	}
	if req.ReturnDate != nil {
		updates["return_date"] = *req.ReturnDate
	}
	if req.ReturnQty != nil {
		updates["return_qty"] = *req.ReturnQty
	}
	if req.SecCertified != nil {
		updates["sec_certified"] = *req.SecCertified
	}

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
