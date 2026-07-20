// Package router 集中注册 Gin 路由，按业务分类拆到各文件。
package router

import (
	"github.com/gin-gonic/gin"

	"ivideo/server/internal/handlers"
)

// Register 注册所有路由。业务接口统一挂在 handlers.APIPrefix(/api/v1)下。
func Register(r *gin.Engine, h *handlers.Handler) {
	api := r.Group(handlers.APIPrefix)

	api.GET("/health", h.Health)

	registerMedia(api, h)     // OpenList / Jellyfin 直读源
	registerResources(api, h) // 资源库 + 分享浏览/转存 + 点播
	registerPlayback(api, h)  // 播放代理 + HLS
	registerSettings(api, h)  // 设置 / 网盘授权
	registerStrm(api, h)      // strm 媒体库 + Emby/Jellyfin 伪文件入口
}
