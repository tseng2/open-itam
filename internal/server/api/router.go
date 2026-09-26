package api

import (
	"time"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/store"
	v1 "itagent/internal/server/api/v1"
	"github.com/gin-gonic/gin"
)

// SetupRouter 初始化总路由树；offlineThreshold 为资产联系状态的离线判定阈值
func SetupRouter(offlineThreshold time.Duration) *gin.Engine {
	r := gin.Default()

	// 基础跨域与健康检查
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	apiV1 := r.Group("/api/v1")
	{
		// 开放路由
		v1.RegisterAuthRoutes(apiV1)
		v1.RegisterAgentRoutes(apiV1)

		// 需要登录认证的路由
		protected := apiV1.Group("/")
		protected.Use(middleware.AuthMiddleware())
		// P2 操作日志：认证之后统一审计变更类请求（POST/PUT/DELETE），
		// 落库失败只走 gin 错误链，绝不阻塞业务响应
		protected.Use(middleware.AuditLog(store.NewGormStore(store.DB)))
		{
			v1.RegisterCompanyRoutes(protected)
			v1.RegisterAssetRoutes(protected, offlineThreshold)
			v1.RegisterAssetRepairRoutes(protected)
			v1.RegisterStorageLendingRoutes(protected)
			v1.RegisterPartRecordRoutes(protected)
			v1.RegisterUserRoutes(protected)
			v1.RegisterProtectionRoutes(protected)
			v1.RegisterDispatchRoutes(protected)
			v1.RegisterWebhookAlertRoutes(protected)
			v1.RegisterStocktakeRoutes(protected)
			v1.RegisterAssetRequestRoutes(protected)
			v1.RegisterDepreciationRoutes(protected)
			v1.RegisterDimensionRoutes(protected)
			v1.RegisterOperationLogRoutes(protected)
			v1.RegisterNotificationRoutes(protected)
			v1.RegisterLicenseRoutes(protected)
			v1.RegisterConsumableRoutes(protected)
			v1.RegisterPortalRoutes(protected)
			v1.RegisterReportRoutes(protected)
			v1.RegisterSoftwarePoolRoutes(protected)
			v1.RegisterSoftwareComplianceRoutes(protected)
		}
	}

	// 免登录移动扫码面（阶段五 P0-β）：项目首个非 JWT 公开面，
	// 鉴权走盘点任务一次性扫码令牌 + 限流中间件，详见 stocktake_public.go
	v1.RegisterStocktakePublicRoutes(apiV1)

	return r
}
