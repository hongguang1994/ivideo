package router

import (
	"github.com/gin-gonic/gin"

	"ivideo/server/internal/handlers"
)

// registerPlayback 注册统一播放代理与 HLS 同源代理。
func registerPlayback(api *gin.RouterGroup, h *handlers.Handler) {
	// 统一播放代理（source=openlist|jellyfin|cache）
	api.GET("/stream", h.Stream)

	// HLS 同源代理（转码 m3u8 + 切片）
	api.GET("/hls", h.HLSPlaylist)
	api.GET("/hls/:name", h.HLSPlaylistFile)
	api.GET("/hlsp/:name", h.HLSSubPlaylist)
	api.GET("/hls-seg", h.HLSSegment)
	api.GET("/hls-seg/:name", h.HLSSegmentFile)
}
