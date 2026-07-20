package handlers

import (
	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resp"
)

// APIPrefix 是所有业务接口的统一前缀。
const APIPrefix = "/api/v1"

// Health 健康检查，同时返回启用了哪些源与当前缓存适配器。
// GET /api/v1/health
func (h *Handler) Health(c *gin.Context) {
	resp.OK(c, gin.H{
		"status":       "ok",
		"sources":      h.sources(),
		"jellyfin":     h.cfg.JellyfinEnabled(),
		"cacheBackend": h.cache.BackendName(),
	})
}

// sources 返回当前启用的来源列表。
func (h *Handler) sources() []string {
	s := []string{"openlist", "cache"}
	if h.cfg.JellyfinEnabled() {
		s = append(s, "jellyfin")
	}
	return s
}
