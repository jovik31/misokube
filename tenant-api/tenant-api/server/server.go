package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func SetupRouter(token string) *gin.Engine {
	r := gin.Default()

	r.GET("/health", HealthHandler)
	r.POST("/tenant", bearerAuth(token), CreateTenantHandler)

	return r
}

func bearerAuth(expected string) gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if expected == "" || len(provided) != len(expected) ||
			subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
