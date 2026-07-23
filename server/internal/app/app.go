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

	// 仅在配置了 API Key 时启用 Jellyfin。
	var jf *jellyfin.Client
	if cfg.JellyfinEnabled() {
		jf = jellyfin.New(cfg.JellyfinBaseURL, cfg.JellyfinAPIKey)
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
	cm.StartCleanup(cfg.CacheCleanInterval, cfg.CacheTTLHours, cfg.CacheMaxBytes, cfg.CacheStopGrace)
	slog.Info("缓存盘适配器已就绪", "backend", backend.Name())

	h := handlers.New(cfg, ol, jf, st, cm)

	// 用 gin.New()（而非 gin.Default()），中间件栈由 router 显式装配，避免重复。
	r := gin.New()
	router.Register(r, h)
	return r, nil
}

// tokenStore 把 store.Store 适配成 backends.TokenStore（读写网盘 token）。
type tokenStore struct{ st store.Store }

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
