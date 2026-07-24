package cache

import (
	"context"
	"fmt"
	"log/slog"

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
	// —— 决策①:准备好没?未就绪则触发转存并等它完成,已就绪记一次访问。——
	// 同步等待:秒转存通常几秒内完成,让播放器一次拿到 302 就行,
	// 别让它撞上 425 —— Jellyfin 探测失败会永久记错媒体信息。
	item, err := m.WaitReady(resourceID, transferWaitTimeout)
	if err != nil {
		return Resolution{}, err
	}
	if item.Status != store.StatusReady || item.CachePath == "" {
		return Resolution{}, fmt.Errorf("资源尚未就绪（%s）", item.Status)
	}

	ctx := context.Background()

	// —— 决策②:原画喂得动吗?——
	// 阿里对**原画下载**限速(实测约 0.5MB/s ≈ 4 Mbps)，但**转码预览流不限速**
	// (实测 52 Mbps)。所以片源码率超过原画通道能力时，硬走原画必然卡，
	// 此时自动降级到转码流 —— 画质让一步，换来能看。
	if kind == KindOriginal && m.originalTooBig(ctx, resourceID, item) {
		kind = KindHLS
	}

	// —— 决策③:去哪取、取什么流?——
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

// originalTooBig 判断「这个片源的码率是否超出原画通道的带宽能力」。
// 码率 = 文件大小×8 / 时长。取不到时长(网盘没给/接口失败)时返回 false ——
// 宁可按原画走，也不要因为一次查询失败就把所有片都降级成转码。
func (m *Manager) originalTooBig(ctx context.Context, resourceID int64, item store.CacheItem) bool {
	if m.originalMaxMbps <= 0 || item.Size <= 0 {
		return false // 阈值为 0 = 关闭自动选流
	}
	dur, ok := m.videoDuration(ctx, resourceID, item.CachePath)
	if !ok || dur <= 0 {
		return false
	}
	mbps := float64(item.Size) * 8 / dur / 1e6
	if mbps <= m.originalMaxMbps {
		return false
	}
	slog.Info("码率超出原画通道能力，自动改走转码流",
		"resource", resourceID, "码率Mbps", fmt.Sprintf("%.1f", mbps),
		"阈值Mbps", m.originalMaxMbps)
	return true
}

// videoDuration 取视频时长，带进程内缓存 —— 播放器会对同一资源发很多次请求，
// 不能每次都去问网盘。
func (m *Manager) videoDuration(ctx context.Context, resourceID int64, cachePath string) (float64, bool) {
	m.mu.Lock()
	if d, hit := m.durations[resourceID]; hit {
		m.mu.Unlock()
		return d, true
	}
	m.mu.Unlock()

	p, ok := m.backend.(MediaProber)
	if !ok {
		return 0, false
	}
	d, err := p.VideoDurationSeconds(ctx, cachePath)
	if err != nil {
		slog.Warn("取视频时长失败，本次按原画处理", "resource", resourceID, "err", err)
		return 0, false
	}

	m.mu.Lock()
	m.durations[resourceID] = d
	m.mu.Unlock()
	return d, true
}
