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
		h.proxyStreamWith(c, res.URL, pan115UA, "", res.Item.Size)
		return
	}
	if strings.HasPrefix(res.Item.CachePath, "quark:") {
		h.proxyStreamWith(c, res.URL, quarkStreamUA, h.cache.StreamCookie(res.Item.CachePath), res.Item.Size)
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

// probeChunkBytes 是补全「开放式 Range」时向上游请求的区间大小。
// 夸克的限速有两头：没有上界会走慢通道(0.1MB/s)，上界过大(实测 ≥512MB)同样掉回慢通道；
// 中间区段才是快通道(实测 32MB→10.6MB/s、256MB→11.7MB/s)。
// 取 256MB：既在快通道内，又让播放器约每 4~5 分钟才需重新发一次 Range 请求
// （取 32MB 时每 35 秒就要重连一次，会周期性卡顿）。
const probeChunkBytes = 256 << 20

// boundedEnd 算出 Range 的上界，并**钳制在文件末尾之内**。
// 关键：夸克/OSS 在上界超出文件大小时会**忽略整个 Range**、从头返回全量（实测
// 返回 200 + 文件开头），导致 ffprobe seek 到 moov 却拿到 ftyp，报 "moov atom not found"。
// size<=0（未知大小）时不钳制，只能按固定块长走。
func boundedEnd(start, size int64) int64 {
	end := start + probeChunkBytes - 1
	if size > 0 && end > size-1 {
		end = size - 1
	}
	return end
}

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
func (h *Handler) proxyStreamWith(c *gin.Context, upstream, ua, cookie string, size int64) {
	clientRange := c.GetHeader("Range")

	// 只有「完全不带 Range」才走分段：夸克对无上界请求按慢速全量流限速
	// （实测 0.1MB/s，带上界 8~10MB/s），分段拉取可绕开。
	// 而 "bytes=N-" 是播放器/ffprobe 的 seek，必须原样透传 ——
	// 若也走分段，seek 到文件尾就要从 N 一路顺序拉到结尾（实测拉了 2.4GB），
	// 既慢又会被客户端中途断开。
	if clientRange == "" {
		h.proxyChunked(c, upstream, ua, cookie, clientRange, size)
		return
	}

	// "bytes=N-"（无上界的 seek）要补一个上界再发给夸克 —— 无上界会触发它的
	// 慢速全量流限速。上界取 probeChunkBytes，足够 ffprobe 读取与播放器起播。
	upRange := clientRange
	if openEnded(upRange) {
		var start int64
		fmt.Sscanf(upRange, "bytes=%d-", &start)
		upRange = fmt.Sprintf("bytes=%d-%d", start, boundedEnd(start, size))
	}

	up, err := h.upstreamRange(c, upstream, ua, cookie, upRange)
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
func (h *Handler) proxyChunked(c *gin.Context, upstream, ua, cookie, clientRange string, size int64) {
	var start int64
	if clientRange != "" {
		fmt.Sscanf(clientRange, "bytes=%d-", &start)
	}

	// 先拉第一段，借它的响应头拿到文件总大小并给客户端定头。
	first, err := h.upstreamRange(c, upstream, ua, cookie,
		fmt.Sprintf("bytes=%d-%d", start, boundedEnd(start, size)))
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, "拉流失败: "+err.Error())
		return
	}
	if first.StatusCode != http.StatusPartialContent && first.StatusCode != http.StatusOK {
		first.Body.Close()
		resp.Fail(c, http.StatusBadGateway,
			fmt.Sprintf("上游拒绝拉流(HTTP %d)，稍后重试", first.StatusCode))
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
			fmt.Sprintf("bytes=%d-%d", pos, boundedEnd(pos, total)))
		if err != nil {
			slog.Warn("分段拉流中断", "pos", pos, "err", err)
			return // 客户端断开或上游出错，正常结束
		}
		// **必须检查状态码**：夸克限流/直链过期时会返回 412/502 的 HTML 错误页，
		// err 却是 nil。若不检查就会把错误页当视频数据拼进流里，
		// 播放器/ffprobe 解析必然失败（实测报 "moov atom not found"）。
		if next.StatusCode != http.StatusPartialContent && next.StatusCode != http.StatusOK {
			next.Body.Close()
			slog.Warn("分段拉流上游异常，停止拼接",
				"pos", pos, "上游状态", next.StatusCode)
			return
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
