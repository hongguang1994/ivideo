package handlers

import (
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/mediaproxy"
	"ivideo/server/internal/resp"

	"ivideo/server/internal/store"
)

// FileGateway 是给 Emby/Jellyfin(strm) 用的「伪文件」入口:
//
//	strm 内容写 http://<ivideo>/api/file/<资源ID>.mkv
//	GET  → 302 跳到原画直链(阿里)，或代理转发(115，UA 绑定不能 302)
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
	resource, err := h.store.GetResource(id)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	// 代理 URL 中的数字 ID 用于稳定寻址；响应仍携带真实文件名和扩展名，
	// 让 Jellyfin/ffprobe 正确探测媒体格式。元数据识别不依赖这个 URL。
	if filename := filepath.Base(resource.FilePath); filename != "." && filename != "/" && filename != "" {
		c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filename}))
	}

	// HEAD:只报存在性/类型,绝不触发转存。
	if c.Request.Method == http.MethodHead {
		contentType := "video/x-matroska"
		extension := ""
		if i := strings.LastIndex(name, "."); i >= 0 {
			extension = strings.ToLower(name[i:])
		}
		switch extension {
		case ".mp4", ".m4v", ".mov":
			contentType = "video/mp4"
		case ".ts":
			contentType = "video/mp2t"
		case ".webm":
			contentType = "video/webm"
		}
		c.Header("Content-Type", contentType)
		c.Header("Accept-Ranges", "bytes")
		// 已转存时附带真实大小；未转存时仍返回 200，让 Jellyfin 知道逻辑媒体源存在。
		if item, err := h.cache.Status(id); err == nil && item.Status == store.StatusReady && item.Size > 0 {
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

	// 115 / 夸克的直链不能 302 直跳给播放器（绑 UA / 绑 cookie），经代理转发。
	if t, ok := h.proxyTarget(res); ok {
		if err := mediaProxy.Stream(c.Writer, c.Request, t); err != nil {
			resp.Fail(c, http.StatusBadGateway, "拉流失败: "+err.Error())
		}
		return
	}

	// 被自动降级成转码流时，跳到**我们自己的** HLS 入口而不是阿里的裸 m3u8 ——
	// 分片要经本服务代理才能取到，这条链路也是既有的、验证过的。
	if res.Kind == cache.KindHLS {
		c.Redirect(http.StatusFound, fmt.Sprintf("%s%s/hls/%d.m3u8", h.cfg.SiteURL, APIPrefix, id))
		return
	}
	c.Redirect(http.StatusFound, res.URL)
}

// 各网盘直链的鉴权要求（必须与适配器取直链时用的一致，否则会被拒）。
const (
	// 115 直链绑定取链时的 UA。
	pan115UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131 Safari/537.36"
	// 夸克官方桌面客户端 UA；夸克接口对 UA 敏感。
	quarkUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"quark-cloud-drive/2.5.20 Chrome/100.0.4896.160 Electron/18.3.5.4-b478491100 Safari/537.36 Channel/pckk_other_ch"
	quarkReferer = "https://pan.quark.cn/"
)

// mediaProxy 是所有上游媒体请求的统一代理；受限直链再启用分段策略。
var mediaProxy = mediaproxy.New(mediaproxy.DefaultChunkBytes)

// proxyTarget 判断该资源是否需要经代理转发，并给出对应的鉴权参数。
// ok=false 表示可以直接 302（阿里）。
func (h *Handler) proxyTarget(res cache.Resolution) (mediaproxy.Target, bool) {
	switch {
	case strings.HasPrefix(res.Item.CachePath, "115:"):
		return mediaproxy.Target{URL: res.URL, UA: pan115UA, Size: res.Item.Size, RangeMode: mediaproxy.RangeChunked}, true
	case strings.HasPrefix(res.Item.CachePath, "quark:"):
		return mediaproxy.Target{
			URL:       res.URL,
			UA:        quarkUA,
			Cookie:    h.cache.StreamCookie(res.Item.CachePath),
			Referer:   quarkReferer,
			Size:      res.Item.Size,
			RangeMode: mediaproxy.RangeChunked,
		}, true
	}
	return mediaproxy.Target{}, false
}
