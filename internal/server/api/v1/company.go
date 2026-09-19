package v1

import (
	"net/http"
	"strconv"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"github.com/gin-gonic/gin"
)

type CompanyHandler struct{}

func RegisterCompanyRoutes(r *gin.RouterGroup) {
	h := &CompanyHandler{}
	companies := r.Group("/companies")
	{
		companies.GET("", h.List)
		companies.POST("", h.Create)
		companies.GET("/:id", h.Get)
	}
}

func (h *CompanyHandler) List(c *gin.Context) {
	var list []model.Company
	if err := store.DB.Find(&list).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to fetch companies")
		return
	}
	Success(c, list)
}

type CreateCompanyRequest struct {
	Name   string `json:"name" binding:"required"`
	Code   string `json:"code" binding:"required"`
	Domain string `json:"domain"`
}

func (h *CompanyHandler) Create(c *gin.Context) {
	var req CreateCompanyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	company := model.Company{
		Name:   req.Name,
		Code:   req.Code,
		Domain: req.Domain,
	}

	if err := store.DB.Create(&company).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "failed to create company")
		return
	}

	Success(c, company)
}

func (h *CompanyHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid company id")
		return
	}

	var company model.Company
	if err := store.DB.First(&company, id).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "company not found")
		return
	}

	Success(c, company)
}
