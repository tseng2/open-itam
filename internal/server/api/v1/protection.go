package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"itagent/internal/shared/password"
)

// ProtectionHandler 防护模块（防退出/防卸载）与卸载验证码的管理接口。
// 配置与密码均归服务端集中管理，安装包不再烧入任何密码
type ProtectionHandler struct {
	store *store.GormStore
}

// RegisterProtectionRoutes 注册管理路由：属高危安全面，仅管理员与超管可读写
func RegisterProtectionRoutes(protected *gin.RouterGroup) {
	adminOnly := protected.Group("/protection")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	h := &ProtectionHandler{store: store.NewGormStore(store.DB)}
	{
		adminOnly.GET("/modules", h.ListModules)
		adminOnly.PUT("/modules/:key", h.UpdateModule)
		adminOnly.POST("/uninstall-code", h.CreateUninstallCode)
	}
}

// moduleStateView 对外视图：只暴露启用开关与是否已设密码，Argon2id 哈希永不回传前端
func moduleStateView(m model.ProtectionModule) gin.H {
	return gin.H{
		"enabled":      m.Enabled,
		"password_set": m.PasswordHash != "",
	}
}

func (h *ProtectionHandler) ListModules(c *gin.Context) {
	quit, err := h.store.GetProtectionModule(c.Request.Context(), model.ProtectionModuleQuit)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询防护模块失败")
		return
	}
	uninstall, err := h.store.GetProtectionModule(c.Request.Context(), model.ProtectionModuleUninstall)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询防护模块失败")
		return
	}
	Success(c, gin.H{
		"quit_protection":      moduleStateView(quit),
		"uninstall_protection": moduleStateView(uninstall),
	})
}

type protectionModuleRequest struct {
	Enabled  *bool  `json:"enabled"`
	Password string `json:"password"`
}

// UpdateModule 更新模块开关与密码：enabled 省略时保持既有值；
// password 省略时保留既有哈希，只改开关不清密码
func (h *ProtectionHandler) UpdateModule(c *gin.Context) {
	key := c.Param("key")
	if key != model.ProtectionModuleQuit && key != model.ProtectionModuleUninstall {
		Fail(c, http.StatusBadRequest, 40001, "未知防护模块")
		return
	}
	var req protectionModuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	existing, err := h.store.GetProtectionModule(c.Request.Context(), key)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询防护模块失败")
		return
	}

	enabled := existing.Enabled
	if req.Enabled != nil {
		// 开启保护但未设密码会被终端 fail-closed 拒绝操作，提前拦截提示
		enabled = *req.Enabled
		if enabled && existing.PasswordHash == "" && req.Password == "" {
			Fail(c, http.StatusBadRequest, 40002, "请先设置密码再启用防护")
			return
		}
	}

	hash := ""
	if req.Password != "" {
		// 密码不落明文：Argon2id 不可逆加盐哈希，与 Agent 端校验共用同一实现
		h2, err := password.Hash(req.Password)
		if err != nil {
			Fail(c, http.StatusBadRequest, 40001, err.Error())
			return
		}
		hash = h2
	}

	if err := h.store.PutProtectionModule(c.Request.Context(), model.ProtectionModule{
		ModuleKey:    key,
		Enabled:      enabled,
		PasswordHash: hash,
	}); err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "保存防护模块失败")
		return
	}
	Success(c, nil)
}

type uninstallCodeRequest struct {
	DeviceID string `json:"device_id" binding:"required"`
}

// CreateUninstallCode 生成随机卸载验证码：绑定设备 + 10 分钟过期 + 单次使用，
// 码值返回给管理员转告终端用户，在线校验走 Agent 通道
func (h *ProtectionHandler) CreateUninstallCode(c *gin.Context) {
	var req uninstallCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	uc, err := h.store.CreateUninstallCode(c.Request.Context(), req.DeviceID, model.UninstallCodeTTL)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "生成卸载验证码失败")
		return
	}
	Success(c, gin.H{
		"code":       uc.Code,
		"device_id":  uc.DeviceID,
		"expires_at": uc.ExpiresAt,
	})
}
