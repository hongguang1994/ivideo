package cache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"ivideo/server/internal/store"
)

const (
	diagnosticSampleBytes = int64(4 << 20)
	diagnosticTimeout     = 20 * time.Second
	diagnosticInterval    = 6 * time.Hour
)

// ProviderDiagnostic 同时描述凭据健康和真实播放能力。
type ProviderDiagnostic struct {
	Provider     string  `json:"provider"`
	TokenHealthy bool    `json:"tokenHealthy"`
	Playable     bool    `json:"playable"`
	SpeedMbps    float64 `json:"speedMbps"`
	SampleBytes  int64   `json:"sampleBytes"`
	DurationMS   int64   `json:"durationMs"`
	CheckedAt    int64   `json:"checkedAt"`
	Message      string  `json:"message"`
}

// DiagnoseProvider 实测令牌；aliyun_open 还会使用一条已转存的阿里资源测速。
func (m *Manager) DiagnoseProvider(provider string) ProviderDiagnostic {
	result := ProviderDiagnostic{Provider: provider, CheckedAt: time.Now().Unix()}
	if err := m.VerifyProvider(provider); err != nil {
		result.Message = err.Error()
		m.saveDiagnostic(result)
		return result
	}
	result.TokenHealthy = true
	result.Message = "令牌有效"
	if provider != "aliyun_open" {
		m.saveDiagnostic(result)
		return result
	}

	p, ok := m.backend.(PlaybackProber)
	if !ok {
		result.Message = "令牌有效；当前网盘不支持播放测速"
		m.saveDiagnostic(result)
		return result
	}
	item, ok := m.playbackProbeItem("aliyun")
	if !ok {
		result.Message = "令牌有效；暂无已转存的阿里视频可用于测速"
		m.saveDiagnostic(result)
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), diagnosticTimeout)
	defer cancel()
	probe, err := p.ProbePlayback(ctx, item.CachePath, diagnosticSampleBytes)
	if err != nil {
		result.Message = "令牌有效，但原画播放失败: " + err.Error()
		m.saveDiagnostic(result)
		return result
	}
	result.Playable = true
	result.SampleBytes = probe.Bytes
	result.DurationMS = probe.Duration.Milliseconds()
	if probe.Duration > 0 {
		result.SpeedMbps = float64(probe.Bytes*8) / probe.Duration.Seconds() / 1e6
	}
	result.Message = fmt.Sprintf("原画可播，实测速率 %.1f Mbps", result.SpeedMbps)
	m.saveDiagnostic(result)
	return result
}

func (m *Manager) playbackProbeItem(provider string) (store.CacheItem, bool) {
	items, err := m.store.ListReady()
	if err != nil {
		return store.CacheItem{}, false
	}
	for i := len(items) - 1; i >= 0; i-- {
		res, err := m.store.GetResource(items[i].ResourceID)
		if err == nil && res.Provider == provider && items[i].CachePath != "" {
			return items[i], true
		}
	}
	return store.CacheItem{}, false
}

func (m *Manager) saveDiagnostic(result ProviderDiagnostic) {
	m.diagnosticMu.Lock()
	m.diagnostics[result.Provider] = result
	m.diagnosticMu.Unlock()
}

// Diagnostic 返回最近一次后台或手动检测结果。
func (m *Manager) Diagnostic(provider string) (ProviderDiagnostic, bool) {
	m.diagnosticMu.RLock()
	defer m.diagnosticMu.RUnlock()
	result, ok := m.diagnostics[provider]
	return result, ok
}

// StartProviderDiagnostics 启动后自动检查，之后每 6 小时复测一次。
func (m *Manager) StartProviderDiagnostics() {
	run := func() {
		providers, err := m.store.ListCredentialProviders()
		if err != nil {
			slog.Warn("读取网盘凭据状态失败", "err", err)
			return
		}
		targets := map[string]bool{}
		for provider, configured := range providers {
			if !configured {
				continue
			}
			switch provider {
			case "aliyun", "aliyun_open", "115", "quark":
				targets[provider] = true
			case "115_cookie":
				targets["115"] = true
			case "quark_cookie":
				targets["quark"] = true
			}
		}
		for provider := range targets {
			result := m.DiagnoseProvider(provider)
			slog.Info("网盘自动诊断", "provider", provider, "token", result.TokenHealthy,
				"playable", result.Playable, "speedMbps", fmt.Sprintf("%.1f", result.SpeedMbps),
				"message", result.Message)
		}
	}
	go func() {
		time.Sleep(10 * time.Second)
		run()
		ticker := time.NewTicker(diagnosticInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
