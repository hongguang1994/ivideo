package cache

import (
	"context"
	"fmt"

	"ivideo/server/internal/store"
)

// StreamKind 表示要给客户端取哪种流。
type StreamKind string

const (
	// KindOriginal 原画直链(mkv/mp4 本体,画质最好;阿里需 Open 令牌)。
	KindOriginal StreamKind = "original"
	// KindHLS 阿里转码 HLS(流畅、兼容浏览器;只需网页版 token,画质有上限)。
	KindHLS StreamKind = "hls"
)

// Resolution 是一次「解析」的结果:取到的可播地址、最终实际类型、以及缓存项。
type Resolution struct {
	URL  string          // 可播地址(原画直链 或 转码 m3u8)
	Kind StreamKind      // 最终实际返回的类型(适配器不支持原画时会回退到转码)
	Item store.CacheItem // 资源缓存项(大小、状态、last_access 等)
}

// Resolve 是「决策层」的统一入口 —— 请求到达代理后,由它决定去哪取数据:
//
//	1) 确保就绪:未转存则按需转存;已就绪则 TouchAccess 记一次访问(即删的依据)
//	2) 按 kind 决定取原画还是转码;适配器不支持原画时自动回退转码
//
// 目前策略很薄(只按传入 kind 二选一)。以后要加的「按文件大小/客户端/带宽
// 自动选流」「大码率换 TV 令牌不限速」「不同网盘不同取法」等,全部收口到这里,
// 各 handler 只管调 Resolve、不再自己判断去哪取。
func (m *Manager) Resolve(resourceID int64, kind StreamKind) (Resolution, error) {
	// —— 决策①:准备好没?未就绪触发转存,已就绪记访问。——
	item, err := m.EnsureReady(resourceID)
	if err != nil {
		return Resolution{}, err
	}
	if item.Status != store.StatusReady || item.CachePath == "" {
		return Resolution{}, fmt.Errorf("资源尚未就绪（%s）", item.Status)
	}

	ctx := context.Background()

	// —— 决策②:去哪取、取什么流?——
	// 原画:适配器实现了 OriginalURLProvider 才走;否则回退到转码。
	if kind == KindOriginal {
		if p, ok := m.backend.(OriginalURLProvider); ok {
			url, err := p.OriginalURL(ctx, item.CachePath)
			if err != nil {
				return Resolution{}, err
			}
			return Resolution{URL: url, Kind: KindOriginal, Item: item}, nil
		}
		// 不支持原画 → 落到下面的转码分支。
	}

	// 转码 HLS(默认 / 原画回退)。
	url, err := m.backend.DirectURL(ctx, item.CachePath)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{URL: url, Kind: KindHLS, Item: item}, nil
}
