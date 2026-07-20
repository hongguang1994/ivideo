package router

import (
	"github.com/gin-gonic/gin"

	"ivideo/server/internal/handlers"
)

// registerSettings 注册设置页与网盘授权。
func registerSettings(api *gin.RouterGroup, h *handlers.Handler) {
	api.GET("/settings/providers", h.Providers)
	api.POST("/settings/token", h.SaveToken)
	api.POST("/auth/aliyun/qr", h.AliyunQR)
	api.POST("/auth/aliyun/qr/status", h.AliyunQRStatus)
}
