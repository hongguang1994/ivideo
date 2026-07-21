package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resp"
)

// CacheItems 列出已缓存(ready)的资源,含标题/大小/上次访问,供缓存面板展示。
// GET /api/cache
func (h *Handler) CacheItems(c *gin.Context) {
	items, err := h.cache.ListCached()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	views := make([]gin.H, 0, len(items))
	var total int64
	for _, it := range items {
		title := ""
		if r, err := h.store.GetResource(it.ResourceID); err == nil {
			title = r.Title
		}
		total += it.Size
		views = append(views, gin.H{
			"resourceId": it.ResourceID,
			"title":      title,
			"size":       it.Size,
			"lastAccess": it.LastAccess,
			"status":     it.Status,
		})
	}
	resp.OK(c, gin.H{"items": views, "totalCount": len(items), "totalBytes": total})
}

// EvictCache 手动删除某资源的缓存(释放自己网盘空间)。
// POST /api/cache/evict  body: {resource}
func (h *Handler) EvictCache(c *gin.Context) {
	var req struct {
		Resource int64 `json:"resource"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Resource <= 0 {
		resp.Fail(c, http.StatusBadRequest, "缺少 resource")
		return
	}
	if err := h.cache.Evict(req.Resource); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{"ok": true})
}
