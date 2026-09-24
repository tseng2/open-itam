package v1

import (
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"itagent/internal/server/webhook"
)

// WebhookAlertHandler 超期/失联 Webhook 告警配置的管理接口（阶段五 A4）。
// 配置存 DB 单例（不塞 server.json），secret 永不回传只回 secret_set
type WebhookAlertHandler struct {
	store *store.GormStore
}

// RegisterWebhookAlertRoutes 注册管理路由：告警通道配置面，仅管理员与超管可读写
func RegisterWebhookAlertRoutes(protected *gin.RouterGroup) {
	adminOnly := protected.Group("/webhook-alerts")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	h := &WebhookAlertHandler{store: store.NewGormStore(store.DB)}
	{
		adminOnly.GET("/config", h.GetConfig)
		adminOnly.PUT("/config", h.UpdateConfig)
		adminOnly.POST("/test", h.TestPush)
	}
}

// webhookConfigView 对外视图：secret 只暴露"是否已设置"，值永不回传
func webhookConfigView(cfg model.WebhookAlertConfig) gin.H {
	return gin.H{
		"enabled":          cfg.Enabled,
		"webhook_url":      cfg.WebhookURL,
		"secret_set":       cfg.Secret != "",
		"cooldown_minutes": cfg.CooldownMinutes,
		"updated_at":       cfg.UpdatedAt,
	}
}

func (h *WebhookAlertHandler) GetConfig(c *gin.Context) {
	cfg, err := h.store.GetWebhookAlertConfig(c.Request.Context())
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询 Webhook 告警配置失败")
		return
	}
	Success(c, webhookConfigView(cfg))
}

// webhookConfigRequest 字段全部可选：省略保持既有值；
// secret 留空保留既有值（只改其他项不会误清签名密钥）
type webhookConfigRequest struct {
	Enabled         *bool   `json:"enabled"`
	WebhookURL      *string `json:"webhook_url"`
	Secret          string  `json:"secret"`
	CooldownMinutes *int    `json:"cooldown_minutes"`
}

func (h *WebhookAlertHandler) UpdateConfig(c *gin.Context) {
	var req webhookConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	existing, err := h.store.GetWebhookAlertConfig(c.Request.Context())
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询 Webhook 告警配置失败")
		return
	}

	cfg := model.WebhookAlertConfig{
		Enabled:         existing.Enabled,
		WebhookURL:      existing.WebhookURL,
		Secret:          existing.Secret,
		CooldownMinutes: existing.CooldownMinutes,
	}
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.WebhookURL != nil {
		cfg.WebhookURL = *req.WebhookURL
	}
	if req.Secret != "" {
		cfg.Secret = req.Secret
	}
	if req.CooldownMinutes != nil {
		if *req.CooldownMinutes < 1 {
			Fail(c, http.StatusBadRequest, 40002, "冷却窗口至少 1 分钟")
			return
		}
		cfg.CooldownMinutes = *req.CooldownMinutes
	}

	if cfg.WebhookURL != "" {
		u, err := url.Parse(cfg.WebhookURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			Fail(c, http.StatusBadRequest, 40003, "Webhook 地址必须是合法的 http(s) URL")
			return
		}
	}
	// 启用前必须有接收地址，否则引擎只能空转
	if cfg.Enabled && cfg.WebhookURL == "" {
		Fail(c, http.StatusBadRequest, 40004, "启用前请先配置 Webhook 地址")
		return
	}

	if err := h.store.PutWebhookAlertConfig(c.Request.Context(), cfg); err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "保存 Webhook 告警配置失败")
		return
	}
	Success(c, webhookConfigView(cfg))
}

// TestPush 手动连通性测试：向已配置的接收端推送 itam.test 载荷。
// 未启用也可测（管理员配置阶段先验通道），接收端非 2xx 原样反馈
func (h *WebhookAlertHandler) TestPush(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, err := h.store.GetWebhookAlertConfig(ctx)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询 Webhook 告警配置失败")
		return
	}
	if cfg.WebhookURL == "" {
		Fail(c, http.StatusBadRequest, 40005, "请先配置 Webhook 地址再测试")
		return
	}
	if err := webhook.Post(ctx, nil, cfg.WebhookURL, cfg.Secret, webhook.TestPayload(time.Now())); err != nil {
		Fail(c, http.StatusBadGateway, 50201, "测试推送失败: "+err.Error())
		return
	}
	Success(c, gin.H{"message": "测试推送已送达"})
}
