package v1

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// auditLoginEvent 登录事件留痕（P2 操作日志）：公开认证面不经审计中间件，
// 在此单独落库。成功记 login，失败记 login_failed；查无此人时
// user_id/company_id 记 0。留痕失败不阻塞登录流程（沿审计旁路先例）
func auditLoginEvent(c *gin.Context, user *model.User, attemptName, action string, status int) {
	log := model.OperationLog{
		Username:  attemptName,
		Action:    action,
		Resource:  "auth",
		Path:      "/api/v1/auth/login",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    status,
	}
	if user != nil {
		log.CompanyID = user.CompanyID
		log.UserID = user.ID
		log.Username = user.Username
		log.Role = user.Role
	}
	if err := store.NewGormStore(store.DB).CreateOperationLog(c.Request.Context(), log); err != nil {
		_ = c.Error(err)
	}
}

func RegisterAuthRoutes(r *gin.RouterGroup) {
	auth := r.Group("/auth")
	{
		auth.POST("/login", handleLogin)
		// 可以在这里添加修改密码、登出(客户端清空token)等接口
	}
}

func handleLogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request format"})
		return
	}

	var user model.User
	err := store.DB.Where("username = ?", req.Username).First(&user).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 初始化逻辑：如果系统里一个用户都没有，且登录账号是 admin，密码是 admin，则自动初始化
			var count int64
			store.DB.Model(&model.User{}).Count(&count)
			if count == 0 && req.Username == "admin" && req.Password == "admin" {
				hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
				
				// 确保有公司记录
				var company model.Company
				if err := store.DB.First(&company).Error; err != nil {
					company = model.Company{Name: "默认集团公司", Code: "DEFAULT"}
					store.DB.Create(&company)
				}
				
				user = model.User{
					CompanyID:    company.ID,
					Username:     "admin",
					RealName:     "系统超级管理员",
					Status:       "active",
					Role:         "super_admin",
					PasswordHash: string(hash),
				}
				store.DB.Create(&user)
			} else {
				auditLoginEvent(c, nil, req.Username, model.OperationActionLoginFailed, http.StatusUnauthorized)
				c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid username or password"})
				return
			}
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "database error"})
			return
		}
	}

	// 校验密码
	if user.PasswordHash == "" {
		if user.Username == "admin" && req.Password == "admin" {
			// 如果 admin 账号此前是由 Agent 上报自动创建的（未设置密码），首次用 admin/admin 登录时自动赋予密码并提升为 super_admin
			hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
			user.PasswordHash = string(hash)
			user.Role = "super_admin"
			user.RealName = "系统超级管理员"
			store.DB.Save(&user)
		} else {
			auditLoginEvent(c, &user, req.Username, model.OperationActionLoginFailed, http.StatusUnauthorized)
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "该账号尚未设置登录密码，请联系管理员"})
			return
		}
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		auditLoginEvent(c, &user, req.Username, model.OperationActionLoginFailed, http.StatusUnauthorized)
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid username or password"})
		return
	}

	if user.Status != "active" {
		auditLoginEvent(c, &user, req.Username, model.OperationActionLoginFailed, http.StatusForbidden)
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "account is disabled"})
		return
	}

	// 生成 Token
	token, err := middleware.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to generate token"})
		return
	}

	auditLoginEvent(c, &user, req.Username, model.OperationActionLogin, http.StatusOK)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"token": token,
			"user": gin.H{
				"id":        user.ID,
				"username":  user.Username,
				"real_name": user.RealName,
				"role":      user.Role,
			},
		},
	})
}
