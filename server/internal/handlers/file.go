package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/store"
)

// FileGateway 是给 Emby/Jellyfin(strm) 用的「伪文件」入口:
//
//	strm 内容写 http://<ivideo>/api/file/<资源ID>.mkv
//	GET  → 302 跳到原画直链(开放接口取,支持 Range,画质最好)
//	HEAD → 只回元信息,**不触发转存**(防止扫描媒体库把所有资源都转存一遍)
func (h *Handler) FileGateway(c *gin.Context) {
	name := c.Param("name")
	// 取扩展名前的数字作为资源 ID:如 "123.mkv" → 123
	base := name
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	id, err := strconv.ParseInt(base, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的资源文件名: " + name})
		return
	}

	// HEAD:只报存在性/类型,绝不触发转存。
	if c.Request.Method == http.MethodHead {
		item, err := h.cache.Status(id)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Content-Type", "video/x-matroska")
		c.Header("Accept-Ranges", "bytes")
		if item.Status == store.StatusReady && item.Size > 0 {
			c.Header("Content-Length", strconv.FormatInt(item.Size, 10))
		}
		c.Status(http.StatusOK)
		return
	}

	// GET:确保已转存,跳转到原画直链。
	url, err := h.cache.OriginalURL(id)
	if err != nil {
		// 未就绪(转存中)时告诉客户端稍后重试。
		c.JSON(http.StatusTooEarly, gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, url)
}
