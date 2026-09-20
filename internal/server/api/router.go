package api

import (
	"itagent/internal/server/api/middleware"
	v1 "itagent/internal/server/api/v1"
	"github.com/gin-gonic/gin"
)

// SetupRouter 初始化总路由树
func SetupRouter() *gin.Engine {
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
			v1.RegisterAssetRoutes(protected)
			v1.RegisterAssetRepairRoutes(protected)
			v1.RegisterStorageLendingRoutes(protected)
			v1.RegisterPartRecordRoutes(protected)
			v1.RegisterUserRoutes(protected)
		}
	}

	return r
}
