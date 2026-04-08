package server

import "github.com/gin-gonic/gin"

func SetupRouter() *gin.Engine {
	r := gin.Default()

	r.GET("/health", HealthHandler)
	r.POST("/tenant", CreateTenantHandler)

	return r
}
