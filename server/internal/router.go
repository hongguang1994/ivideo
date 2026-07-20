package internal

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/cache/backends"
	"ivideo/server/internal/config"
	"ivideo/server/internal/handlers"
	"ivideo/server/internal/jellyfin"
	"ivideo/server/internal/openlist"
	"ivideo/server/internal/store"
)

// NewRouter 组装依赖与 Gin 路由。
func NewRouter(cfg config.Config, st *store.Store) (*gin.Engine, error) {
	ol := openlist.New(cfg.OpenListBaseURL, cfg.OpenListUsername, cfg.OpenListPassword)

	// 仅在配置了 API Key 时启用 Jellyfin。
	var jf *jellyfin.Client
	if cfg.JellyfinEnabled() {
		jf = jellyfin.New(cfg.JellyfinBaseURL, cfg.JellyfinAPIKey)
		log.Printf("已启用 Jellyfin 源：%s", cfg.JellyfinBaseURL)
	} else {
		log.Printf("未配置 Jellyfin，仅提供 OpenList 源")
	}

	// 缓存盘适配器 + 管理器 + 清理任务。
	backend, err := backends.New(cfg, tokenStore{st})
	if err != nil {
		return nil, err
	}
	cm := cache.NewManager(st, backend, cfg.CacheDir)
	cm.StartCleanup(cfg.CacheCleanInterval, cfg.CacheTTLHours, cfg.CacheMaxBytes)
	log.Printf("缓存盘适配器：%s", backend.Name())

	h := handlers.New(cfg, ol, jf, st, cm)

	r := gin.Default()

	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":       "ok",
			"sources":      sources(cfg),
			"jellyfin":     cfg.JellyfinEnabled(),
			"cacheBackend": backend.Name(),
		})
	})

	api := r.Group("/api")
	{
		// OpenList / Jellyfin 直读源
		api.GET("/videos", h.ListVideos)
		api.GET("/image", h.Image)

		// 资源目录 + 按需转存缓存
		api.GET("/resources", h.ListResources)
		api.POST("/resources", h.AddResource)
		api.GET("/play", h.Play)

		// 统一播放代理（source=openlist|jellyfin|cache）
		api.GET("/stream", h.Stream)
		// HLS 同源代理（转码 m3u8 + 切片）
		api.GET("/hls", h.HLSPlaylist)
		api.GET("/hls/:name", h.HLSPlaylistFile)
		api.GET("/hls-seg", h.HLSSegment)

		// 设置 / 网盘授权
		api.GET("/settings/providers", h.Providers)
		api.POST("/settings/token", h.SaveToken)
		api.POST("/auth/aliyun/qr", h.AliyunQR)
		api.POST("/auth/aliyun/qr/status", h.AliyunQRStatus)

		// Emby/Jellyfin(strm) 伪文件入口 → 302 原画直链;HEAD 不触发转存
		api.GET("/file/:name", h.FileGateway)
		api.HEAD("/file/:name", h.FileGateway)
		// 生成 strm 媒体库
		api.POST("/strm/generate", h.GenerateStrm)
	}

	return r, nil
}

// tokenStore 把 *store.Store 适配成 backends.TokenStore（读写网盘 token）。
type tokenStore struct{ st *store.Store }

func (t tokenStore) GetToken(provider string) string {
	cr, _, err := t.st.GetCredential(provider)
	if err != nil {
		return ""
	}
	return cr.Token
}

func (t tokenStore) SaveToken(provider, token string) error {
	return t.st.SetCredentialToken(provider, token)
}

// sources 返回当前启用的来源列表。
func sources(cfg config.Config) []string {
	s := []string{"openlist", "cache"}
	if cfg.JellyfinEnabled() {
		s = append(s, "jellyfin")
	}
	return s
}
