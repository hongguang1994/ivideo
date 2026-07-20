package router

import (
	"github.com/gin-gonic/gin"

	"ivideo/server/internal/handlers"
)

// registerStrm 注册 strm 媒体库生成与 Emby/Jellyfin 伪文件入口。
func registerStrm(api *gin.RouterGroup, h *handlers.Handler) {
	// Emby/Jellyfin(strm) 伪文件入口 → 302 原画直链;HEAD 不触发转存
	api.GET("/file/:name", h.FileGateway)
	api.HEAD("/file/:name", h.FileGateway)
	// 生成 strm 媒体库
	api.POST("/strm/generate", h.GenerateStrm)
}
