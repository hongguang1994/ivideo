package router

import (
	"github.com/gin-gonic/gin"

	"ivideo/server/internal/handlers"
)

// registerMedia 注册直读源(OpenList / Jellyfin)的浏览与图片代理。
func registerMedia(api *gin.RouterGroup, h *handlers.Handler) {
	api.GET("/videos", h.ListVideos)
	api.GET("/image", h.Image)
}
