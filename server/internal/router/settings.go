package router

import (
	"github.com/gin-gonic/gin"

	"ivideo/server/internal/handlers"
)

// registerSettings 注册设置页与网盘授权。
func registerSettings(api *gin.RouterGroup, h *handlers.Handler) {
	api.GET("/settings/providers", h.Providers)
	api.POST("/settings/providers/check", h.CheckProvider) // 实测校验令牌健康度
	api.POST("/settings/token", h.SaveToken)
	api.POST("/auth/aliyun/qr", h.AliyunQR)
	api.POST("/auth/aliyun/qr/status", h.AliyunQRStatus)
	// 开放接口(原画直链)扫码授权 —— 阿里官方 OAuth，需自备 client_id/secret
	api.POST("/auth/aliyun/open/qr", h.AliyunOpenQR)
	api.POST("/auth/aliyun/open/qr/status", h.AliyunOpenQRStatus)
}
