// Package backends 汇集各网盘的缓存盘适配器，并提供工厂。
package backends

import (
	"fmt"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/config"
)

// New 按配置创建缓存盘适配器。tokens 用于读写扫码后持久化的网盘 token（可为 nil）。
func New(cfg config.Config, tokens TokenStore) (cache.CacheBackend, error) {
	switch cfg.CacheBackend {
	case "fake", "":
		return NewFake(), nil
	case "aliyun":
		// 阿里为默认盘，115 / 夸克并存（无凭据时休眠，播到对应资源才用）。
		ali := NewAliyun(cfg, tokens)
		return NewDispatcher(ali, map[string]cache.CacheBackend{
			"115":   NewPan115(cfg, tokens),
			"quark": NewQuark(cfg, tokens),
		}), nil
	default:
		return nil, fmt.Errorf("未知缓存盘适配器: %s", cfg.CacheBackend)
	}
}
