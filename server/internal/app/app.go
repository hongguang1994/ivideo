// Package app 负责启动引导：装配各依赖，组装 Gin 引擎。
package app

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/cache/backends"
	"ivideo/server/internal/config"
	"ivideo/server/internal/handlers"
	"ivideo/server/internal/jellyfin"
	"ivideo/server/internal/openlist"
	"ivideo/server/internal/router"
	"ivideo/server/internal/store"
)

// New 装配依赖并返回配置好路由的 Gin 引擎。
func New(cfg config.Config, st store.Store) (*gin.Engine, error) {
	ol := openlist.New(cfg.OpenListBaseURL, cfg.OpenListUsername, cfg.OpenListPassword)

	// Jellyfin 专用 Key 优先从数据库读取，首次初始化后无需把密钥写进配置文件。
	jellyfinKey := cfg.JellyfinAPIKey
	if saved, found, err := st.GetCredential("jellyfin_api"); err == nil && found && saved.Token != "" {
		jellyfinKey = saved.Token
	}
	// 仅在配置了 API Key 时启用 Jellyfin。
	var jf *jellyfin.Client
	if cfg.JellyfinBaseURL != "" && jellyfinKey != "" {
		jf = jellyfin.New(cfg.JellyfinBaseURL, jellyfinKey)
		slog.Info("已启用 Jellyfin 源", "baseURL", cfg.JellyfinBaseURL)
	} else {
		slog.Info("未配置 Jellyfin，仅提供 OpenList 源")
	}

	// 缓存盘适配器 + 管理器 + 清理任务。
	backend, err := backends.New(cfg, tokenStore{st})
	if err != nil {
		return nil, err
	}
	cm := cache.NewManager(st, backend, cfg.CacheDir)
	// 有 Jellyfin 时启用「会话感知清理」：正在播放/暂停的资源不删，停止且过宽限期才删。
	if jf != nil {
		cm.SetSessionSource(cache.NewJellyfinSessions(jf, cfg.MediaDir))
	}
	// 自动选流：码率超过阈值的片源改走转码流（原画通道被阿里限速，喂不动高码率）。
	cm.SetOriginalMaxMbps(cfg.OriginalMaxMbps)
	// 令牌保活：定时预热令牌，避免闲置失效 + 第一次播放不用等刷新。
	cm.StartTokenRefresh(cfg.TokenRefreshMin)
	cm.StartProviderDiagnostics()
	cm.StartCleanup(cfg.CacheCleanInterval, cfg.CacheTTLHours, cfg.CacheMaxBytes, cfg.CacheStopGrace)
	slog.Info("缓存盘适配器已就绪", "backend", backend.Name())

	importService, metadataService, workflowService := buildMediaModules(cfg, st, cm, jf)
	discovery, rssSource := buildDiscovery(cfg, st, cm, metadataService)
	h := handlers.New(cfg, ol, jf, st, cm, importService, metadataService, workflowService, discovery, rssSource)

	// strm 媒体库自动维护：启动时生成一次 + 定时兜底（导入完成后也会即时触发）。
	h.StartAutoStrm(cfg.StrmAutoInterval)
	h.StartShareChecks(cfg.ShareCheckInterval)
	h.StartImportScheduler()
	h.StartRSSScheduler()
	if jf != nil {
		if err := jf.EnsureLibraries(cfg.MediaDir); err != nil {
			slog.Warn("确保 Jellyfin 媒体库失败", "err", err)
		} else if err := jf.RefreshLibrary(); err != nil {
			slog.Warn("首次扫描 Jellyfin 媒体库失败", "err", err)
		} else {
			slog.Info("Jellyfin 媒体库已就绪")
		}
		if removed, err := jf.RemoveMissingItems(cfg.MediaDir); err != nil {
			slog.Warn("清理 Jellyfin 旧索引失败", "err", err)
		} else if removed > 0 {
			slog.Info("已清理 Jellyfin 旧索引", "removed", removed)
			if err := jf.RefreshLibrary(); err != nil {
				slog.Warn("清理旧索引后重新扫描失败", "err", err)
			}
		}
	}

	// 用 gin.New()（而非 gin.Default()），中间件栈由 router 显式装配，避免重复。
	r := gin.New()
	router.Register(r, h, cfg)
	return r, nil
}

// tokenStore 把凭据仓储适配成 backends.TokenStore（读写网盘 token）。
type tokenStore struct{ st store.CredentialRepository }

func (t tokenStore) GetToken(provider string) string {
	cr, _, err := t.st.GetCredential(provider)
	if err != nil {
		return ""
	}
	return cr.Token
}

func (t tokenStore) GetTokenExtra(provider string) string {
	cr, _, err := t.st.GetCredential(provider)
	if err != nil {
		return ""
	}
	return cr.Extra
}

func (t tokenStore) SaveToken(provider, token string) error {
	return t.st.SetCredentialToken(provider, token)
}
