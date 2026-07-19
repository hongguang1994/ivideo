package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Stream 按来源取播放地址并代理转发，透传 Range 以支持进度拖动。
// GET /api/stream?source=openlist&path=/some/video.mp4
// GET /api/stream?source=jellyfin&id=<itemId>
func (h *Handler) Stream(c *gin.Context) {
	switch c.DefaultQuery("source", "openlist") {
	case "jellyfin":
		h.streamJellyfin(c)
	case "cache":
		h.streamCache(c)
	default:
		h.streamOpenList(c)
	}
}

// streamCache 播放已转存进自己网盘的资源；未就绪则触发转存并返回 425。
// GET /api/stream?source=cache&resource=<id>
func (h *Handler) streamCache(c *gin.Context) {
	id, ok := parseID(c, "resource")
	if !ok {
		return
	}
	rawURL, err := h.cache.StreamURL(id)
	if err != nil {
		// 尚未就绪：告诉客户端稍后重试（425 Too Early）。
		c.JSON(http.StatusTooEarly, gin.H{"error": err.Error()})
		return
	}
	h.proxyStream(c, rawURL)
}

// streamOpenList 取网盘直链并代理转发。
func (h *Handler) streamOpenList(c *gin.Context) {
	rel := c.Query("path")
	if rel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 path 参数"})
		return
	}
	rawURL, err := h.ol.RawURL(h.resolve(rel))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	h.proxyStream(c, rawURL)
}

// streamJellyfin 代理转发 Jellyfin 的直连播放流。
func (h *Handler) streamJellyfin(c *gin.Context) {
	if h.jf == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 Jellyfin"})
		return
	}
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 id 参数"})
		return
	}
	h.proxyStream(c, h.jf.StreamURL(id))
}
