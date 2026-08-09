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
	api.GET("/settings/jellyfin", h.JellyfinStatus)
	api.POST("/settings/jellyfin/initialize", h.InitializeJellyfin)
	api.POST("/settings/jellyfin/refresh-images", h.RefreshJellyfinImages)
	api.GET("/settings/metadata", h.MetadataStatus)
	api.POST("/settings/metadata/token", h.SaveMetadataToken)
	api.GET("/settings/search", h.SearchSettings)
	api.PUT("/settings/search/sources/:id", h.UpdateSearchSource)
	api.POST("/settings/search/github", h.SaveGitHubToken)
	api.DELETE("/settings/search/github", h.DeleteGitHubToken)
	api.GET("/settings/search/rss", h.RSSSettings)
	api.PUT("/settings/search/rss", h.SaveRSSSettings)
	api.POST("/settings/search/rss/run", h.RunRSSCollection)
	api.GET("/settings/import", h.ImportSettings)
	api.PUT("/settings/import", h.SaveImportSettings)
	api.GET("/imports/status", h.ImportTaskStatus)
	api.POST("/imports/run", h.RunImportTask)
	api.POST("/metadata/scrape", h.ScrapeMetadata)
	api.GET("/media-groups", h.MediaGroups)
	api.GET("/media-groups/:id", h.MediaGroup)
	api.POST("/media-groups/:id/confirm", h.ConfirmMediaGroup)
	api.POST("/media-groups/:id/review", h.ReviewMediaGroup)
	api.POST("/media-groups/:id/candidates", h.SearchMediaGroupCandidates)
	api.POST("/auth/aliyun/qr", h.AliyunQR)
	api.POST("/auth/aliyun/qr/status", h.AliyunQRStatus)
	// 开放接口(原画直链)扫码授权 —— 阿里官方 OAuth，需自备 client_id/secret
	api.POST("/auth/aliyun/open/qr", h.AliyunOpenQR)
	api.POST("/auth/aliyun/open/qr/status", h.AliyunOpenQRStatus)

	// 115 网页扫码登录 —— 拿网页态 cookie，用于转存分享
	api.POST("/auth/115/qr", h.Pan115QR)
	api.POST("/auth/115/qr/status", h.Pan115QRStatus)

	// 夸克扫码登录 —— 夸克开放 API 需 secret 签名，故整条链路走网页 cookie
	api.POST("/auth/quark/qr", h.QuarkQR)
	api.POST("/auth/quark/qr/status", h.QuarkQRStatus)
}
