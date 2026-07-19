package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Image 代理 Jellyfin 的海报图，避免前端直连 Jellyfin。
// GET /api/image?source=jellyfin&id=<itemId>
func (h *Handler) Image(c *gin.Context) {
	if c.DefaultQuery("source", "jellyfin") != "jellyfin" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 jellyfin 海报代理"})
		return
	}
	if h.jf == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 Jellyfin"})
		return
	}
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 id 参数"})
		return
	}
	h.proxyStream(c, h.jf.ImageURL(id))
}
