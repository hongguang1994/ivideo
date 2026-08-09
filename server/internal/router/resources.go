package router

import (
	"github.com/gin-gonic/gin"

	"ivideo/server/internal/handlers"
)

// registerResources 注册资源库、分享浏览/转存、点播。
func registerResources(api *gin.RouterGroup, h *handlers.Handler) {
	api.GET("/resources", h.ListResources)
	api.POST("/resources", h.AddResource)
	api.POST("/resources/import", h.ImportShare)
	api.GET("/search/resources", h.SearchResources)
	api.GET("/search/resources/ws", h.StreamSearch)
	api.GET("/search/github/sources", h.ListGitHubSources)
	api.GET("/search/github/resources", h.ListGitHubResources)
	api.POST("/search/github/resources/sync", h.SyncGitHubResources)
	api.GET("/share/browse", h.BrowseShare)
	api.POST("/share/save", h.SaveShareItem)
	api.GET("/play", h.Play)

	// 缓存/即删管理面板
	api.GET("/cache", h.CacheItems)
	api.POST("/cache/evict", h.EvictCache)

	// 分享库(收藏的网盘分享链接)
	api.GET("/shares", h.ListShares)
	api.POST("/shares", h.AddShare)
	api.POST("/shares/batch", h.AddSharesBatch)
	api.PUT("/shares/:id", h.UpdateShare)
	api.DELETE("/shares/:id", h.DeleteShare)
	api.POST("/shares/check", h.CheckAllShares)
	api.POST("/shares/:id/check", h.CheckOneShare)
}
