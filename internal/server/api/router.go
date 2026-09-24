package api

import (
	"time"

	"itagent/internal/server/api/middleware"
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
		{
			v1.RegisterCompanyRoutes(protected)
			v1.RegisterAssetRoutes(protected, offlineThreshold)
			v1.RegisterAssetRepairRoutes(protected)
			v1.RegisterStorageLendingRoutes(protected)
			v1.RegisterPartRecordRoutes(protected)
			v1.RegisterUserRoutes(protected)
			v1.RegisterProtectionRoutes(protected)
			v1.RegisterDispatchRoutes(protected)
		}
	}

	return r
}
