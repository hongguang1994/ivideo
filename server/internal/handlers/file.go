package handlers

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/resp"

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
		resp.Fail(c, http.StatusBadRequest, "非法的资源文件名: "+name)
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

	// GET:经 Resolve 决策(按需转存 + 记访问 + 选流),拿到实际取到的流类型后跳转。
	res, err := h.cache.Resolve(id, cache.KindOriginal)
	if err != nil {
		// 未就绪(转存中)时告诉客户端稍后重试。
		resp.Fail(c, http.StatusTooEarly, err.Error())
		return
	}
	c.Header("X-Stream-Kind", string(res.Kind)) // original / hls：让外部看到实际给了哪种流
	slog.Info("播放解析", "resource", id, "kind", res.Kind, "size", res.Item.Size)
	c.Redirect(http.StatusFound, res.URL)
}
