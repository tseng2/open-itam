package v1

import (
	"net/http"
	"strconv"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type UserHandler struct{}

func RegisterUserRoutes(r *gin.RouterGroup) {
	h := &UserHandler{}
	users := r.Group("/users")
	{
		users.GET("", h.List)
		users.POST("", h.Create)
		users.PUT("/:id", h.Update)
		users.DELETE("/:id", h.Delete)
	}
}

type ListUserQuery struct {
	CompanyID int64  `form:"company_id"`
	Role      string `form:"role"`
	Status    string `form:"status"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page,default=1"`
	PageSize  int    `form:"page_size,default=20"`
}

func (h *UserHandler) List(c *gin.Context) {
	var query ListUserQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	db := store.DB.Model(&model.User{})

	if query.CompanyID > 0 {
		db = db.Where("company_id = ?", query.CompanyID)
	}
	if query.Role != "" {
		db = db.Where("role = ?", query.Role)
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if query.Keyword != "" {
		db = db.Where("username LIKE ? OR real_name LIKE ? OR job_number LIKE ?", "%"+query.Keyword+"%", "%"+query.Keyword+"%", "%"+query.Keyword+"%")
	}

	var total int64
	db.Count(&total)

	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	offset := (query.Page - 1) * query.PageSize

	var list []model.User
	if err := db.Offset(offset).Limit(query.PageSize).Order("id asc").Find(&list).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "failed to query users")
		return
	}

	Success(c, gin.H{
		"total": total,
		"items": list,
	})
}

type CreateUserRequest struct {
	CompanyID    int64  `json:"company_id" binding:"required"`
	DepartmentID *int64 `json:"department_id"`
	Username     string `json:"username" binding:"required"`
	Password     string `json:"password" binding:"required"`
	RealName     string `json:"real_name" binding:"required"`
	JobNumber    string `json:"job_number"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	Status       string `json:"status"`
}

func (h *UserHandler) Create(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	var count int64
	store.DB.Model(&model.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		Fail(c, http.StatusBadRequest, 40002, "用户名已存在")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "密码哈希生成失败")
		return
	}

	role := req.Role
	if role == "" {
		role = "user"
	}
	status := req.Status
	if status == "" {
		status = "active"
	}

	user := model.User{
		CompanyID:    req.CompanyID,
		DepartmentID: req.DepartmentID,
		Username:     req.Username,
		RealName:     req.RealName,
		JobNumber:    req.JobNumber,
		Email:        req.Email,
		Role:         role,
		Status:       status,
		PasswordHash: string(hash),
	}

	if err := store.DB.Create(&user).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50003, "创建用户失败")
		return
	}

	Success(c, user)
}

type UpdateUserRequest struct {
	RealName  string `json:"real_name"`
	JobNumber string `json:"job_number"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	Password  string `json:"password"`
}

func (h *UserHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40001, "invalid user id")
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40002, err.Error())
		return
	}

	var user model.User
	if err := store.DB.First(&user, id).Error; err != nil {
		Fail(c, http.StatusNotFound, 40003, "用户不存在")
		return
	}

	if req.RealName != "" {
		user.RealName = req.RealName
	}
	if req.JobNumber != "" {
		user.JobNumber = req.JobNumber
	}
	if req.Email != "" {
		user.Email = req.Email
	}
	if req.Role != "" {
		user.Role = req.Role
	}
	if req.Status != "" {
		user.Status = req.Status
	}
	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err == nil {
			user.PasswordHash = string(hash)
		}
	}

	if err := store.DB.Save(&user).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50004, "更新用户失败")
		return
	}

	Success(c, user)
}

func (h *UserHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40001, "invalid user id")
		return
	}

	if err := store.DB.Delete(&model.User{}, id).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50005, "删除用户失败")
		return
	}

	Success(c, gin.H{"id": id})
}
