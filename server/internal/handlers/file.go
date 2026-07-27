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

// probeChunkBytes 是客户端未指定 Range 时，向上游请求的区间大小。
// 取 32MB：足够 ffprobe 解析媒体信息，又能走夸克的分段快通道。
const probeChunkBytes = 32 << 20

// openEnded 判断 Range 是否「没有上界」（空串，或形如 bytes=0- / bytes=123-）。
func openEnded(rng string) bool {
	if rng == "" {
		return true
	}
	return strings.HasSuffix(strings.TrimSpace(rng), "-")
}

// quarkStreamUA 必须与夸克取直链时用的 UA 一致（夸克接口对 UA 敏感）。
const quarkStreamUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
	"quark-cloud-drive/2.5.20 Chrome/100.0.4896.160 Electron/18.3.5.4-b478491100 Safari/537.36 Channel/pckk_other_ch"

// proxyStreamWith 用指定 UA/cookie 拉上游直链并流式转发，透传 Range 与关键响应头。
// 供 115（绑 UA）和夸克（绑 cookie）使用；与 handlers.go 里那个不带鉴权的
// proxyStream 区分开，避免影响 OpenList/Jellyfin 既有链路。
func (h *Handler) proxyStreamWith(c *gin.Context, upstream, ua, cookie string) {
	clientRange := c.GetHeader("Range")

	// 客户端要「开放式区间」(无 Range 或 bytes=N-)时不能原样转发：
	// 夸克对没有上界的请求按慢速全量流限速（实测 0.1MB/s，带上界是 8~10MB/s），
	// ffprobe 探测正是发 "bytes=0-"，这就是开播要等 ~20 秒的根源。
	// 这里改为内部按 chunk 连续分段拉取，再拼成一条流喂给客户端。
	if openEnded(clientRange) {
		h.proxyChunked(c, upstream, ua, cookie, clientRange)
		return
	}

	up, err := h.upstreamRange(c, upstream, ua, cookie, clientRange)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, "拉流失败: "+err.Error())
		return
	}
	defer up.Body.Close()

	copyStreamHeaders(c, up)
	c.Status(up.StatusCode)
	_, _ = io.Copy(c.Writer, up.Body)
}

// upstreamRange 用指定 Range 向上游发起一次请求。
func (h *Handler) upstreamRange(c *gin.Context, upstream, ua, cookie, rng string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstream, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Referer", "https://pan.quark.cn/")
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	return proxyClient.Do(req)
}

// proxyChunked 把「开放式区间」拆成连续的有界分段拉取，边拉边写给客户端。
// 客户端只看到一条正常的流（无 Range → 200，bytes=N- → 206）。
func (h *Handler) proxyChunked(c *gin.Context, upstream, ua, cookie, clientRange string) {
	var start int64
	if clientRange != "" {
		fmt.Sscanf(clientRange, "bytes=%d-", &start)
	}

	// 先拉第一段，借它的响应头拿到文件总大小并给客户端定头。
	first, err := h.upstreamRange(c, upstream, ua, cookie,
		fmt.Sprintf("bytes=%d-%d", start, start+probeChunkBytes-1))
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, "拉流失败: "+err.Error())
		return
	}
	total := totalFromContentRange(first.Header.Get("Content-Range"))

	c.Header("Accept-Ranges", "bytes")
	if v := first.Header.Get("Content-Type"); v != "" {
		c.Header("Content-Type", v)
	}
	if total > 0 {
		c.Header("Content-Length", strconv.FormatInt(total-start, 10))
		if clientRange != "" {
			c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, total-1, total))
		}
	}
	if clientRange == "" {
		c.Status(http.StatusOK)
	} else {
		c.Status(http.StatusPartialContent)
	}

	pos := start
	up := first
	for {
		n, _ := io.Copy(c.Writer, up.Body)
		up.Body.Close()
		pos += n
		if n == 0 || (total > 0 && pos >= total) {
			return // 传完了
		}
		next, err := h.upstreamRange(c, upstream, ua, cookie,
			fmt.Sprintf("bytes=%d-%d", pos, pos+probeChunkBytes-1))
		if err != nil {
			slog.Warn("分段拉流中断", "pos", pos, "err", err)
			return // 客户端断开或上游出错，正常结束
		}
		up = next
	}
}

// copyStreamHeaders 透传上游的关键响应头。
func copyStreamHeaders(c *gin.Context, up *http.Response) {
	for _, hk := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := up.Header.Get(hk); v != "" {
			c.Header(hk, v)
		}
	}
}

// totalFromContentRange 从 "bytes 0-33554431/1038876196" 解析出文件总大小，失败返回 0。
func totalFromContentRange(cr string) int64 {
	i := strings.LastIndex(cr, "/")
	if i < 0 {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
