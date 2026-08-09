package handlers

import (
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resp"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/config"
	"ivideo/server/internal/importer"
	"ivideo/server/internal/jellyfin"
	"ivideo/server/internal/mediaproxy"
	"ivideo/server/internal/mediaworkflow"
	"ivideo/server/internal/metadata"
	"ivideo/server/internal/openlist"
	"ivideo/server/internal/resourcesearch"
	"ivideo/server/internal/store"
)

// Handler 聚合请求处理所需的依赖。
type Handler struct {
	cfg            config.Config
	ol             *openlist.Client
	jf             *jellyfin.Client // 可能为 nil（未配置 Jellyfin 时）
	store          Repository
	cache          *cache.Manager
	importer       *importer.Service
	metadata       *metadata.Service
	workflow       *mediaworkflow.Service
	discovery      *resourcesearch.Engine
	shareCheckMu   sync.Mutex
	importRunMu    sync.Mutex
	importStatusMu sync.RWMutex
	importStatus   importTaskStatus
	importCursor   int
}

// Repository is the API layer's aggregate. Business modules receive narrower
// interfaces and never depend on this HTTP-facing composition.
type Repository interface {
	store.ResourceRepository
	store.ShareRepository
	store.CredentialRepository
	store.SettingsRepository
	store.MediaRepository
}

// logShareCheck 是共享的后台任务日志入口，避免健康检查失败影响用户请求。
func logShareCheck(message string, args ...any) { slog.Info(message, args...) }

// New 创建 Handler。所有业务模块均由 app 装配后注入。
func New(
	cfg config.Config, ol *openlist.Client, jf *jellyfin.Client, st Repository, cm *cache.Manager,
	importService *importer.Service, metadataService *metadata.Service, workflowService *mediaworkflow.Service,
	discovery *resourcesearch.Engine,
) *Handler {
	return &Handler{
		cfg: cfg, ol: ol, jf: jf, store: st, cache: cm,
		importer: importService, metadata: metadataService, workflow: workflowService, discovery: discovery,
	}
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
		resp.Fail(c, http.StatusBadRequest, "缺少或非法的 "+key+" 参数")
		return 0, false
	}
	return id, true
}

// itoa 是 int64 转字符串的简写。
func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// proxyStream 统一经媒体代理转发 OpenList、Jellyfin 与 HLS 切片。
func (h *Handler) proxyStream(c *gin.Context, upstream string) {
	if err := mediaProxy.Stream(c.Writer, c.Request, mediaproxy.Target{URL: upstream}); err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
	}
}
