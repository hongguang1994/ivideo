package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resp"
)

// Image 代理 Jellyfin 的海报图，避免前端直连 Jellyfin。
// GET /api/image?source=jellyfin&id=<itemId>
func (h *Handler) Image(c *gin.Context) {
	if c.DefaultQuery("source", "jellyfin") != "jellyfin" {
		resp.Fail(c, http.StatusBadRequest, "仅支持 jellyfin 海报代理")
		return
	}
	if h.jf == nil {
		resp.Fail(c, http.StatusServiceUnavailable, "未配置 Jellyfin")
		return
	}
	id := c.Query("id")
	if id == "" {
		resp.Fail(c, http.StatusBadRequest, "缺少 id 参数")
		return
	}
	h.proxyStream(c, h.jf.ImageURL(id))
}
