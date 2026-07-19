package handlers

import (
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/config"
	"ivideo/server/internal/jellyfin"
	"ivideo/server/internal/openlist"
	"ivideo/server/internal/store"
)

// Handler 聚合请求处理所需的依赖。
type Handler struct {
	cfg   config.Config
	ol    *openlist.Client
	jf    *jellyfin.Client // 可能为 nil（未配置 Jellyfin 时）
	store *store.Store
	cache *cache.Manager
}

// New 创建 Handler。jf 为 nil 表示未启用 Jellyfin。
func New(cfg config.Config, ol *openlist.Client, jf *jellyfin.Client, st *store.Store, cm *cache.Manager) *Handler {
	return &Handler{cfg: cfg, ol: ol, jf: jf, store: st, cache: cm}
}

// isVideo 判断文件名是否为受支持的视频格式。
func (h *Handler) isVideo(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	for _, e := range h.cfg.VideoExts {
		if e == ext {
			return true
		}
	}
	return false
}

// resolve 把前端传入的相对路径拼到配置的视频根目录下。
func (h *Handler) resolve(p string) string {
	if p == "" {
		p = "/"
	}
	return path.Join("/", h.cfg.OpenListRoot, p)
}

// parseID 从查询参数解析整型 ID，失败时已写好 400 响应并返回 false。
func parseID(c *gin.Context, key string) (int64, bool) {
	raw := c.Query(key)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少或非法的 " + key + " 参数"})
		return 0, false
	}
	return id, true
}

// itoa 是 int64 转字符串的简写。
func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// proxyStream 向 upstream 发起 GET，透传 Range，并把响应转发给客户端。
// OpenList 直链和 Jellyfin 播放流共用这段逻辑。
func (h *Handler) proxyStream(c *gin.Context, upstream string) {
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstream, nil)
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

	for _, hk := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified"} {
		if v := resp.Header.Get(hk); v != "" {
			c.Header(hk, v)
		}
	}
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}
