package handlers

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// 允许被 HLS 代理的上游主机(防止变成任意 URL 的开放代理)。
var hlsAllowedHosts = []string{"aliyundrive.net", "aliyundrive.cloud", "alipan.com", "aliyuncs.com"}

func hlsHostAllowed(host string) bool {
	host = strings.ToLower(host)
	for _, s := range hlsAllowedHosts {
		if strings.Contains(host, s) {
			return true
		}
	}
	return false
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的资源文件名: " + name})
		return
	}
	h.servePlaylist(c, id)
}

// HLSPlaylist 代理并改写 m3u8。
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
		c.JSON(http.StatusTooEarly, gin.H{"error": err.Error()})
		return
	}
	h.renderPlaylist(c, u)
}

// renderPlaylist 拉取上游 m3u8,把其中的地址改写成走本站同源代理,规避阿里 CDN 跨域。
func (h *Handler) renderPlaylist(c *gin.Context, m3u8URL string) {
	base, err := url.Parse(m3u8URL)
	if err != nil || !hlsHostAllowed(base.Host) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 m3u8 地址"})
		return
	}

	resp, err := http.Get(m3u8URL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

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
		if strings.Contains(strings.ToLower(abs), ".m3u8") {
			b.WriteString("/api/hls?url=" + url.QueryEscape(abs)) // 子播放列表再改写
		} else {
			b.WriteString("/api/hls-seg?u=" + url.QueryEscape(abs)) // 切片走代理
		}
		b.WriteByte('\n')
	}

	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.String(http.StatusOK, b.String())
}

// HLSSegment 代理单个切片(或子资源)。
// GET /api/hls-seg?u=<绝对地址>
func (h *Handler) HLSSegment(c *gin.Context) {
	raw := c.Query("u")
	u, err := url.Parse(raw)
	if err != nil || !hlsHostAllowed(u.Host) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法切片地址"})
		return
	}
	h.proxyStream(c, raw) // 复用带 Range 透传的代理
}
