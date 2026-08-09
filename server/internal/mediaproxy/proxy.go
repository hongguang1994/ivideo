// Package mediaproxy 是「带鉴权的分段流式代理」：把网盘的受限直链转发给播放器。
//
// 为什么需要它：有些网盘的直链不能直接 302 给播放器 ——
//   - 115：直链绑定取链时的 UA，播放器自带 UA 会被拒（403）
//   - 夸克：直链绑定 cookie，且每次取链都会刷新 __puus
//
// 分段拉取的由来（都是实测踩出来的）：
//   - 夸克对**没有上界**的 Range（无 Range 头 / bytes=N-）按慢速全量流限速，
//     实测 0.1MB/s，而带上界是 8~11MB/s；
//   - 但上界**过大**（实测 ≥512MB）同样掉回慢通道；
//   - 上界**越过文件末尾**时更糟：夸克会忽略整个 Range、从文件开头返回，
//     导致播放器 seek 到文件尾的 moov 却拿到文件头，报 "moov atom not found"。
//
// 所以对开放式区间，这里在内部拆成「有界且不越界」的块连续拉取，
// 对外仍呈现为一条完整的流（客户端无感知）。
package mediaproxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultChunkBytes 是分段拉取的块大小。
// 取 256MB：既在夸克的快通道区间内，又让播放器约每 4~5 分钟才需重发一次 Range
// （取 32MB 时每 35 秒就要重连一次，会周期性卡顿）。
const DefaultChunkBytes int64 = 256 << 20

// Target 描述一次转发的上游目标与所需鉴权。
type Target struct {
	URL       string    // 上游直链
	UA        string    // 必须与取直链时用的 UA 一致（115/夸克都对 UA 敏感）
	Cookie    string    // 需要 cookie 鉴权的网盘（夸克）传入；否则留空
	Referer   string    // 需要防盗链校验的网盘传入
	Size      int64     // 文件总大小，用于把 Range 上界钳制在文件内；未知传 0
	RangeMode RangeMode // Range 透传或内部拆分；零值为直接透传
}

// RangeMode 定义代理处理客户端 Range 的策略。
type RangeMode uint8

const (
	// RangePassthrough 把客户端 Range 原样交给上游，适用于 OpenList/Jellyfin/HLS。
	RangePassthrough RangeMode = iota
	// RangeChunked 把开放式 Range 拆为有界片段，适用于 115/夸克受限直链。
	RangeChunked
)

// Proxy 执行转发。零值不可用，请用 New。
type Proxy struct {
	client *http.Client
	chunk  int64
}

// New 创建代理。chunkBytes<=0 时用 DefaultChunkBytes。
// http.Client 不设整体超时 —— 播放是长连接，超时会把正常播放掐断；
// 但限制首包等待，避免失效直链永久占用连接。
func New(chunkBytes int64) *Proxy {
	if chunkBytes <= 0 {
		chunkBytes = DefaultChunkBytes
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 20 * time.Second
	return &Proxy{client: &http.Client{Transport: transport}, chunk: chunkBytes}
}

// Get 发起非流式上游 GET，供 HLS 播放列表等调用复用鉴权与超时策略。
// 调用方负责关闭响应体。
func (p *Proxy) Get(ctx context.Context, t Target) (*http.Response, error) {
	return p.fetch(ctx, t, "")
}

// Stream 把上游内容转发给客户端。
// 返回 error 表示**还没开始写响应**就失败了（调用方可自行渲染错误）；
// 一旦开始写响应，中途出错只记日志，返回 nil。
func (p *Proxy) Stream(w http.ResponseWriter, r *http.Request, t Target) error {
	clientRange := r.Header.Get("Range")

	// 开放式区间（无 Range 或 bytes=N-）：客户端要的是「从 N 到文件结尾」的完整内容，
	// 必须给足长度 —— 只回一个固定块就结束会让播放器/ffmpeg 读到意外 EOF，转码中途失败。
	if t.RangeMode == RangeChunked && OpenEnded(clientRange) {
		return p.streamChunked(w, r, t, clientRange)
	}

	up, err := p.fetch(r.Context(), t, clientRange)
	if err != nil {
		return err
	}
	defer up.Body.Close()

	copyHeaders(w, up)
	w.WriteHeader(up.StatusCode)
	if _, err := io.Copy(w, up.Body); err != nil && r.Context().Err() == nil {
		slog.Warn("上游流读取中断", "err", err)
	}
	return nil
}

// streamChunked 把开放式区间拆成连续的有界分段拉取，边拉边写给客户端。
func (p *Proxy) streamChunked(w http.ResponseWriter, r *http.Request, t Target, clientRange string) error {
	start := rangeStart(clientRange)

	// 先拉第一段，借它的响应头拿到文件总大小并给客户端定头。
	first, err := p.fetch(r.Context(), t, fmt.Sprintf("bytes=%d-%d", start, BoundedEnd(start, t.Size, p.chunk)))
	if err != nil {
		return err
	}
	if !validChunkResponse(first, start) {
		first.Body.Close()
		return fmt.Errorf("上游未按请求范围返回数据(HTTP %d)", first.StatusCode)
	}
	total := TotalFromContentRange(first.Header.Get("Content-Range"))
	if total <= 0 {
		total = t.Size
	}

	writeChunkedHeaders(w, first, clientRange, start, total)

	pos := start
	up := first
	for {
		n, copyErr := io.Copy(w, up.Body)
		up.Body.Close()
		pos += n
		if copyErr != nil {
			if r.Context().Err() != nil {
				return nil // 客户端已断开，不再请求下一段
			}
			slog.Warn("分段拉流读取中断，尝试续传", "pos", pos, "err", copyErr)
		}
		if n == 0 || (total > 0 && pos >= total) {
			return nil // 传完了，或客户端断开
		}
		next, err := p.fetch(r.Context(), t, fmt.Sprintf("bytes=%d-%d", pos, BoundedEnd(pos, total, p.chunk)))
		if err != nil {
			slog.Warn("分段拉流中断", "pos", pos, "err", err)
			return nil // 客户端断开或上游出错，正常结束
		}
		// **必须检查状态码**：网盘限流/直链过期时会返回 4xx/5xx 的 HTML 错误页，
		// err 却是 nil。不检查就会把错误页当视频数据拼进流里，播放器必然解析失败。
		if !validChunkResponse(next, pos) {
			next.Body.Close()
			slog.Warn("分段拉流上游异常，停止拼接", "pos", pos, "上游状态", next.StatusCode)
			return nil
		}
		up = next
	}
}

// fetch 用指定 Range 向上游发起一次请求。
func (p *Proxy) fetch(ctx context.Context, t Target, rng string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return nil, err
	}
	if t.UA != "" {
		req.Header.Set("User-Agent", t.UA)
	}
	if t.Cookie != "" {
		req.Header.Set("Cookie", t.Cookie)
	}
	if t.Referer != "" {
		req.Header.Set("Referer", t.Referer)
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	return p.client.Do(req)
}

// writeChunkedHeaders 给分段流写响应头：对外表现为一条完整的流。
func writeChunkedHeaders(w http.ResponseWriter, first *http.Response, clientRange string, start, total int64) {
	h := w.Header()
	h.Set("Accept-Ranges", "bytes")
	if v := first.Header.Get("Content-Type"); v != "" {
		h.Set("Content-Type", v)
	}
	if total > 0 {
		h.Set("Content-Length", strconv.FormatInt(total-start, 10))
		if clientRange != "" {
			h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, total-1, total))
		}
	}
	if clientRange == "" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusPartialContent)
	}
}

// copyHeaders 透传上游的关键响应头。
func copyHeaders(w http.ResponseWriter, up *http.Response) {
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range",
		"Accept-Ranges", "Last-Modified", "ETag"} {
		if v := up.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
}

// validChunkResponse 确保上游真的遵守了内部 Range 请求。
// 有的 CDN 在 Range 无效时会回 200 并从文件头开始；若继续拼接会污染播放流。
func validChunkResponse(resp *http.Response, wantStart int64) bool {
	start, _, _, ok := ParseContentRange(resp.Header.Get("Content-Range"))
	return resp.StatusCode == http.StatusPartialContent && ok && start == wantStart
}

// OpenEnded 判断 Range 是否「没有上界」（空串，或形如 bytes=0- / bytes=123-）。
func OpenEnded(rng string) bool {
	if rng == "" {
		return true
	}
	return strings.HasSuffix(strings.TrimSpace(rng), "-")
}

// rangeStart 从 "bytes=N-" 取起始偏移；解析不出按 0。
func rangeStart(rng string) int64 {
	var start int64
	if rng != "" {
		_, _ = fmt.Sscanf(rng, "bytes=%d-", &start)
	}
	return start
}

// BoundedEnd 算出 Range 的上界，并**钳制在文件末尾之内**。
// 关键：夸克/OSS 在上界超出文件大小时会忽略整个 Range、从头返回全量（实测返回 200 +
// 文件开头），导致播放器 seek 到 moov 却拿到 ftyp，报 "moov atom not found"。
// size<=0（未知大小）时无法钳制，只能按固定块长走。
func BoundedEnd(start, size, chunk int64) int64 {
	end := start + chunk - 1
	if size > 0 && end > size-1 {
		end = size - 1
	}
	return end
}

// TotalFromContentRange 从 "bytes 0-33554431/1038876196" 解析文件总大小，失败返回 0。
func TotalFromContentRange(cr string) int64 {
	_, _, total, ok := ParseContentRange(cr)
	if !ok {
		return 0
	}
	return total
}

// ParseContentRange 解析 "bytes 0-1023/4096"；总大小未知时 total 为 0。
func ParseContentRange(cr string) (start, end, total int64, ok bool) {
	var length string
	if _, err := fmt.Sscanf(strings.TrimSpace(cr), "bytes %d-%d/%s", &start, &end, &length); err != nil || start < 0 || end < start {
		return 0, 0, 0, false
	}
	if length == "*" {
		return start, end, 0, true
	}
	total, err := strconv.ParseInt(length, 10, 64)
	if err != nil || total <= end {
		return 0, 0, 0, false
	}
	return start, end, total, true
}
