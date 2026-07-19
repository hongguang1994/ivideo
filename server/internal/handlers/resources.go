package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/store"
)

// ListResources 列出资源目录（收集来的分享链接）。
// GET /api/resources
func (h *Handler) ListResources(c *gin.Context) {
	items, err := h.store.ListResources()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// AddResource 新增一条资源。
// POST /api/resources  body: {title, provider, shareUrl, sharePwd?, filePath?, poster?, overview?}
func (h *Handler) AddResource(c *gin.Context) {
	var r store.Resource
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if r.Title == "" || r.ShareURL == "" || r.Provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title、provider、shareUrl 必填"})
		return
	}
	id, err := h.store.AddResource(r)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	r.ID = id
	c.JSON(http.StatusOK, r)
}

// Play 触发/查询某资源的转存状态；就绪则返回可播地址。
// GET /api/play?resource=<id>
func (h *Handler) Play(c *gin.Context) {
	id, ok := parseID(c, "resource")
	if !ok {
		return
	}
	item, err := h.cache.EnsureReady(id)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	resp := gin.H{"status": item.Status}
	switch item.Status {
	case store.StatusReady:
		resp["streamUrl"] = "/api/stream?source=cache&resource=" + itoa(id)
	case store.StatusTransferring, store.StatusUncached:
		resp["message"] = "正在转存到网盘，请稍候…"
	case store.StatusFailed:
		resp["message"] = "转存失败：" + item.Error
	}
	c.JSON(http.StatusOK, resp)
}
