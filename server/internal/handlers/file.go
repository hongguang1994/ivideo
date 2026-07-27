package handlers

import (
	"fmt"
	"io"
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

	// 115 / 夸克的直链都不能 302 直跳给播放器：
	//   115  —— 直链绑 UA，播放器 UA 不匹配会 403
	//   夸克 —— 直链绑 cookie（每次取直链都会刷新 __puus），不带会被 CDN 拒（412）
	// 两者都由 ivideo 用正确的 UA/cookie 拉流、透传 Range 后转发。
	if strings.HasPrefix(res.Item.CachePath, "115:") {
		h.proxyStreamWith(c, res.URL, pan115UA, "")
		return
	}
	if strings.HasPrefix(res.Item.CachePath, "quark:") {
		h.proxyStreamWith(c, res.URL, quarkStreamUA, h.cache.StreamCookie(res.Item.CachePath))
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

// pan115UA 必须与 pan115 取直链时用的 UA 完全一致（115 直链绑定该 UA）。
const pan115UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131 Safari/537.36"

var proxyClient = &http.Client{} // 流式转发，不设整体超时

// quarkStreamUA 必须与夸克取直链时用的 UA 一致（夸克接口对 UA 敏感）。
const quarkStreamUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
	"quark-cloud-drive/2.5.20 Chrome/100.0.4896.160 Electron/18.3.5.4-b478491100 Safari/537.36 Channel/pckk_other_ch"

// proxyStreamWith 用指定 UA/cookie 拉上游直链并流式转发，透传 Range 与关键响应头。
// 供 115（绑 UA）和夸克（绑 cookie）使用；与 handlers.go 里那个不带鉴权的
// proxyStream 区分开，避免影响 OpenList/Jellyfin 既有链路。
func (h *Handler) proxyStreamWith(c *gin.Context, upstream, ua, cookie string) {
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstream, nil)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	req.Header.Set("User-Agent", ua)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Referer", "https://pan.quark.cn/")
	}
	// Range 处理：客户端给了就透传；没给也主动带 "bytes=0-"。
	// 夸克对**不带 Range** 的请求返回慢速全量流（实测 ffprobe 探测因此拖到 ~20 秒，
	// 而带 Range 只要 0.5 秒），带上后走分段快通道。
	clientRange := c.GetHeader("Range")
	rng := clientRange
	if rng == "" {
		rng = "bytes=0-"
	}
	req.Header.Set("Range", rng)
	up, err := proxyClient.Do(req)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, "拉流失败: "+err.Error())
		return
	}
	defer up.Body.Close()

	for _, hk := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := up.Header.Get(hk); v != "" {
			c.Header(hk, v)
		}
	}
	status := up.StatusCode
	// 客户端没要 Range，但我们为了走快通道向上游要了 —— 对外仍应是完整响应 200，
	// 且不该带 Content-Range，否则播放器会以为这是分段。
	if clientRange == "" && status == http.StatusPartialContent {
		c.Writer.Header().Del("Content-Range")
		status = http.StatusOK
	}
	c.Status(status)
	_, _ = io.Copy(c.Writer, up.Body)
}
