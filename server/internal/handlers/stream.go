package handlers

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Stream 取网盘直链并代理转发给前端，透传 Range 以支持进度拖动。
// GET /api/stream?path=/some/video.mp4
func (h *Handler) Stream(c *gin.Context) {
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

	// 向网盘直链发起请求，透传客户端的 Range 头。
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if r := c.GetHeader("Range"); r != "" {
		req.Header.Set("Range", r)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	// 透传关键响应头，让浏览器 <video> 正确处理分段与拖动。
	for _, hk := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified"} {
		if v := resp.Header.Get(hk); v != "" {
			c.Header(hk, v)
		}
	}
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}
