// Package router 集中注册 Gin 路由，按业务分类拆到各文件。
package router

import (
	"github.com/gin-gonic/gin"

	"ivideo/server/internal/handlers"
	"ivideo/server/internal/middleware"
)

// Register 装配中间件并注册所有路由。业务接口统一挂在 handlers.APIPrefix(/api/v1)下。
func Register(r *gin.Engine, h *handlers.Handler) {
	// 全局中间件栈：Recovery 兜底 panic（返回 500 不崩），Logger 记录访问日志。
	r.Use(gin.Recovery(), middleware.Logger())

	api := r.Group(handlers.APIPrefix)

	api.GET("/health", h.Health)

	registerMedia(api, h)     // OpenList / Jellyfin 直读源
	registerResources(api, h) // 资源库 + 分享浏览/转存 + 点播
	registerPlayback(api, h)  // 播放代理 + HLS
	registerSettings(api, h)  // 设置 / 网盘授权
	registerStrm(api, h)      // strm 媒体库 + Emby/Jellyfin 伪文件入口
}
