package handlers

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resp"
)

// hlsHostAllowed 判断上游主机是否在配置的白名单里(防止变成任意 URL 的开放代理)。
func (h *Handler) hlsHostAllowed(host string) bool {
	host = strings.ToLower(host)
	for _, s := range h.cfg.HLSAllowedHosts {
		if s != "" && strings.Contains(host, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// encodeUpstream 把上游地址编码进路径。
// 用 base64url(字符集只有 A-Za-z0-9-_，不含点)，这样整个 URL 里
// **唯一的点就是结尾的扩展名** —— ffmpeg 的 HLS allowed_extensions 检查靠
// “最后一个点之后的内容”判断扩展名，查询串里带点会让它认不出而拒绝打开切片。
func encodeUpstream(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeUpstream 从 "<base64url>.ext" 还原上游地址。
func (h *Handler) decodeUpstream(name string) (string, bool) {
	if i := strings.LastIndex(name, "."); i > 0 {
		name = name[:i]
	}
	b, err := base64.RawURLEncoding.DecodeString(name)
	if err != nil {
		return "", false
	}
	raw := string(b)
	u, err := url.Parse(raw)
	if err != nil || !h.hlsHostAllowed(u.Host) {
		return "", false
	}
	return raw, true
}

// HLSPlaylistFile 是带 .m3u8 后缀的入口,供 strm / 播放器按扩展名识别。
// GET /api/hls/<资源ID>.m3u8
func (h *Handler) HLSPlaylistFile(c *gin.Context) {
	name := c.Param("name")
	base := name
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	id, err := strconv.ParseInt(base, 10, 64)
	if err != nil || id <= 0 {
		resp.Fail(c, http.StatusBadRequest, "非法的资源文件名: "+name)
		return
	}
	h.servePlaylist(c, id)
}

// HLSSubPlaylist 处理被改写过的子播放列表。
// GET /api/hlsp/<base64url>.m3u8
func (h *Handler) HLSSubPlaylist(c *gin.Context) {
	raw, ok := h.decodeUpstream(c.Param("name"))
	if !ok {
		resp.Fail(c, http.StatusBadRequest, "非法的子播放列表地址")
		return
	}
	h.renderPlaylist(c, raw)
}

// HLSPlaylist 兼容旧的查询参数入口。
// GET /api/hls?resource=<id>   或   GET /api/hls?url=<绝对 m3u8 地址>
func (h *Handler) HLSPlaylist(c *gin.Context) {
	if raw := c.Query("url"); raw != "" {
		h.renderPlaylist(c, raw)
		return
	}
	id, ok := parseID(c, "resource")
	if !ok {
		return
	}
	h.servePlaylist(c, id)
}

// servePlaylist 确保资源已转存,取到 HLS 地址后改写输出。
func (h *Handler) servePlaylist(c *gin.Context, id int64) {
	u, err := h.cache.StreamURL(id)
	if err != nil {
		// 未就绪(转存中):让客户端稍后重试。
		resp.Fail(c, http.StatusTooEarly, err.Error())
		return
	}
	h.renderPlaylist(c, u)
}

// renderPlaylist 拉取上游 m3u8,把其中的地址改写成走本站同源代理。
func (h *Handler) renderPlaylist(c *gin.Context, m3u8URL string) {
	base, err := url.Parse(m3u8URL)
	if err != nil || !h.hlsHostAllowed(base.Host) {
		resp.Fail(c, http.StatusBadRequest, "非法的 m3u8 地址")
		return
	}

	httpResp, err := http.Get(m3u8URL)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	defer httpResp.Body.Close()
	body, _ := io.ReadAll(httpResp.Body)

	var b strings.Builder
	for _, line := range strings.Split(string(body), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		ref, e := url.Parse(t)
		if e != nil {
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		abs := base.ResolveReference(ref).String()
		enc := encodeUpstream(abs)
		if strings.Contains(strings.ToLower(abs), ".m3u8") {
			b.WriteString(APIPrefix + "/hlsp/" + enc + ".m3u8") // 子播放列表继续改写
		} else {
			b.WriteString(APIPrefix + "/hls-seg/" + enc + ".ts") // 切片走代理，扩展名明确
		}
		b.WriteByte('\n')
	}

	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.String(http.StatusOK, b.String())
}

// HLSSegmentFile 代理单个切片。
// GET /api/hls-seg/<base64url>.ts
func (h *Handler) HLSSegmentFile(c *gin.Context) {
	raw, ok := h.decodeUpstream(c.Param("name"))
	if !ok {
		resp.Fail(c, http.StatusBadRequest, "非法切片地址")
		return
	}
	h.proxyStream(c, raw)
}

// HLSSegment 兼容旧的查询参数入口。
// GET /api/hls-seg?u=<绝对地址>
func (h *Handler) HLSSegment(c *gin.Context) {
	raw := c.Query("u")
	u, err := url.Parse(raw)
	if err != nil || !h.hlsHostAllowed(u.Host) {
		resp.Fail(c, http.StatusBadRequest, "非法切片地址")
		return
	}
	h.proxyStream(c, raw)
}
