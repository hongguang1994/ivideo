package handlers

import (
	"io"
	"net/http"
	"net/url"
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

// HLSPlaylist 代理并改写 m3u8：把相对切片地址改成走本站同源代理,规避阿里 CDN 跨域。
// GET /api/hls?resource=<id>   或   GET /api/hls?url=<绝对 m3u8 地址>
func (h *Handler) HLSPlaylist(c *gin.Context) {
	var m3u8URL string
	if raw := c.Query("url"); raw != "" {
		m3u8URL = raw
	} else {
		id, ok := parseID(c, "resource")
		if !ok {
			return
		}
		u, err := h.cache.StreamURL(id)
		if err != nil {
			c.JSON(http.StatusTooEarly, gin.H{"error": err.Error()})
			return
		}
		m3u8URL = u
	}

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
