package backends

import (
	"context"
	"strings"

	"ivideo/server/internal/cache"
)

// Dispatcher 让多个网盘适配器并存：按资源的 provider / 缓存路径前缀，把每次调用
// 路由到对应的适配器。Manager 照常只持有一个 CacheBackend（即本 Dispatcher），
// 无需改动，就能同时支持阿里 + 115。
//
// 缓存路径编码：默认盘(def，如阿里)写裸路径（向后兼容库里已有数据）；非默认盘
// 写「<provider>:<内层路径>」前缀，路由时据此还原并分发。
type Dispatcher struct {
	def    cache.CacheBackend            // 默认盘：无前缀路径走它（阿里）
	byName map[string]cache.CacheBackend // provider -> 适配器（如 "115"）
}

// NewDispatcher 组装分发器。def 为默认盘，extra 是额外的 provider->适配器。
func NewDispatcher(def cache.CacheBackend, extra map[string]cache.CacheBackend) *Dispatcher {
	return &Dispatcher{def: def, byName: extra}
}

// routeByPath 按缓存路径前缀选适配器，返回适配器和剥掉前缀的内层路径。
func (d *Dispatcher) routeByPath(cachePath string) (cache.CacheBackend, string) {
	for name, b := range d.byName {
		if strings.HasPrefix(cachePath, name+":") {
			return b, cachePath[len(name)+1:]
		}
	}
	return d.def, cachePath
}

// ---- CacheBackend ----

func (d *Dispatcher) Name() string { return "dispatcher" }

func (d *Dispatcher) Transfer(ctx context.Context, share cache.ShareRef) (cache.TransferResult, error) {
	b, prefix := d.def, ""
	if bb, ok := d.byName[share.Provider]; ok {
		b, prefix = bb, share.Provider+":"
	}
	tr, err := b.Transfer(ctx, share)
	if err != nil {
		return tr, err
	}
	tr.CachePath = prefix + tr.CachePath
	return tr, nil
}

func (d *Dispatcher) DirectURL(ctx context.Context, cachePath string) (string, error) {
	b, inner := d.routeByPath(cachePath)
	return b.DirectURL(ctx, inner)
}

func (d *Dispatcher) Delete(ctx context.Context, cachePath string) error {
	b, inner := d.routeByPath(cachePath)
	return b.Delete(ctx, inner)
}

func (d *Dispatcher) EmptyTrash(ctx context.Context) error {
	// 默认盘清一次即可；其余盘的 EmptyTrash 目前是 no-op。
	err := d.def.EmptyTrash(ctx)
	for _, b := range d.byName {
		_ = b.EmptyTrash(ctx)
	}
	return err
}

func (d *Dispatcher) Quota(ctx context.Context) (used, total int64, err error) {
	return d.def.Quota(ctx)
}

// ---- 可选能力：按路径/ provider 分发 ----

// OriginalURL 供 strm 原画入口用；适配器不支持原画时回退其 DirectURL。
func (d *Dispatcher) OriginalURL(ctx context.Context, cachePath string) (string, error) {
	b, inner := d.routeByPath(cachePath)
	if p, ok := b.(cache.OriginalURLProvider); ok {
		return p.OriginalURL(ctx, inner)
	}
	return b.DirectURL(ctx, inner)
}

// VideoDurationSeconds 供自动选流估码率；适配器不支持则返回未知(0)。
func (d *Dispatcher) VideoDurationSeconds(ctx context.Context, cachePath string) (float64, error) {
	b, inner := d.routeByPath(cachePath)
	if p, ok := b.(cache.MediaProber); ok {
		return p.VideoDurationSeconds(ctx, inner)
	}
	return 0, nil
}

// ProbePlayback 按缓存路径路由到对应网盘的限量播放探测。
func (d *Dispatcher) ProbePlayback(ctx context.Context, cachePath string, sampleBytes int64) (cache.PlaybackProbeResult, error) {
	b, inner := d.routeByPath(cachePath)
	if p, ok := b.(cache.PlaybackProber); ok {
		return p.ProbePlayback(ctx, inner, sampleBytes)
	}
	return cache.PlaybackProbeResult{}, cache.ErrNotImplemented
}

// Verify 按 provider 路由校验。"115"->115；其余(aliyun/aliyun_open)->默认盘。
func (d *Dispatcher) Verify(ctx context.Context, provider string) error {
	b := d.def
	if bb, ok := d.byName[provider]; ok {
		b = bb
	}
	if v, ok := b.(cache.TokenVerifier); ok {
		return v.Verify(ctx, provider)
	}
	return cache.ErrNotImplemented
}

// RefreshTokens 保活所有实现了 TokenRefresher 的盘。
func (d *Dispatcher) RefreshTokens(ctx context.Context) error {
	var firstErr error
	all := append([]cache.CacheBackend{d.def}, mapValues(d.byName)...)
	for _, b := range all {
		if r, ok := b.(cache.TokenRefresher); ok {
			if err := r.RefreshTokens(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// ListShare 按 share.Provider 路由浏览。
func (d *Dispatcher) ListShare(ctx context.Context, share cache.ShareRef, subPath string) ([]cache.ShareEntry, error) {
	b := d.backendForProvider(share.Provider)
	if p, ok := b.(cache.ShareLister); ok {
		return p.ListShare(ctx, share, subPath)
	}
	return nil, cache.ErrNotImplemented
}

// WalkShare 按 share.Provider 路由高效遍历。
func (d *Dispatcher) WalkShare(ctx context.Context, share cache.ShareRef) ([]cache.ShareEntry, error) {
	b := d.backendForProvider(share.Provider)
	if p, ok := b.(cache.ShareWalker); ok {
		return p.WalkShare(ctx, share)
	}
	return nil, cache.ErrNotImplemented
}

// SaveToFolder 按 share.Provider 路由手动转存。
func (d *Dispatcher) SaveToFolder(ctx context.Context, share cache.ShareRef, srcPath, targetFolder string) error {
	b := d.backendForProvider(share.Provider)
	if p, ok := b.(cache.ShareSaver); ok {
		return p.SaveToFolder(ctx, share, srcPath, targetFolder)
	}
	return cache.ErrNotImplemented
}

// StreamCookieFor 返回某缓存路径对应盘的拉流 cookie（不支持则空串）。
func (d *Dispatcher) StreamCookieFor(cachePath string) string {
	b, _ := d.routeByPath(cachePath)
	if p, ok := b.(cache.StreamCookieProvider); ok {
		return p.StreamCookie()
	}
	return ""
}

// IsHLS 沿用默认盘的判断（阿里 HLS 语义；115 直链是文件，不受此影响）。
func (d *Dispatcher) IsHLS() bool {
	if p, ok := d.def.(cache.HLSStreamer); ok {
		return p.IsHLS()
	}
	return false
}

func (d *Dispatcher) backendForProvider(provider string) cache.CacheBackend {
	if b, ok := d.byName[provider]; ok {
		return b
	}
	return d.def
}

func mapValues(m map[string]cache.CacheBackend) []cache.CacheBackend {
	out := make([]cache.CacheBackend, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
