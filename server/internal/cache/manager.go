package cache

import (
	"context"
	"fmt"
	"log"
	"sync"

	"ivideo/server/internal/store"
)

// Manager 负责“确保已转存”的编排：点播 → 若未缓存则后台转存（并发去重）→ 就绪后给直链。
type Manager struct {
	store    store.Store
	backend  CacheBackend
	cacheDir string

	mu       sync.Mutex
	inflight map[int64]bool // 正在转存的资源 ID，用于去重
}

// NewManager 创建缓存管理器。
func NewManager(st store.Store, backend CacheBackend, cacheDir string) *Manager {
	return &Manager{
		store:    st,
		backend:  backend,
		cacheDir: cacheDir,
		inflight: make(map[int64]bool),
	}
}

// EnsureReady 确保某资源已转存。
// 已就绪则刷新访问时间并返回；否则**非阻塞**地触发后台转存，返回当前状态（转存中）。
func (m *Manager) EnsureReady(resourceID int64) (store.CacheItem, error) {
	res, err := m.store.GetResource(resourceID)
	if err != nil {
		return store.CacheItem{}, fmt.Errorf("资源不存在: %w", err)
	}

	item, err := m.store.GetCacheItem(resourceID)
	if err != nil {
		return store.CacheItem{}, err
	}
	// 就绪 = 已转存(cache_path 已写)。直链在播放时实时取（HLS 地址会过期，不入库）。
	if item.Status == store.StatusReady && item.CachePath != "" {
		_ = m.store.TouchAccess(resourceID)
		return item, nil
	}

	m.startTransfer(res)

	// 返回触发后的最新状态（大概率是 transferring）。
	item, _ = m.store.GetCacheItem(resourceID)
	return item, nil
}

// StreamURL 确保已转存后，实时取一个可播直链（HLS 地址短时有效，每次现取）。
func (m *Manager) StreamURL(resourceID int64) (string, error) {
	item, err := m.EnsureReady(resourceID)
	if err != nil {
		return "", err
	}
	if item.Status != store.StatusReady || item.CachePath == "" {
		return "", fmt.Errorf("资源尚未就绪（%s）", item.Status)
	}
	return m.backend.DirectURL(context.Background(), item.CachePath)
}

// OriginalURL 取「原画直链」(供 Emby/Jellyfin 的 strm 使用)。
// 适配器需实现 OriginalURLProvider，否则回退到 DirectURL。
func (m *Manager) OriginalURL(resourceID int64) (string, error) {
	item, err := m.EnsureReady(resourceID)
	if err != nil {
		return "", err
	}
	if item.Status != store.StatusReady || item.CachePath == "" {
		return "", fmt.Errorf("资源尚未就绪（%s）", item.Status)
	}
	if p, ok := m.backend.(OriginalURLProvider); ok {
		return p.OriginalURL(context.Background(), item.CachePath)
	}
	return m.backend.DirectURL(context.Background(), item.CachePath)
}

// ListShare 列出分享内目录(适配器需实现 ShareLister)。
func (m *Manager) ListShare(share ShareRef, subPath string) ([]ShareEntry, error) {
	p, ok := m.backend.(ShareLister)
	if !ok {
		return nil, fmt.Errorf("当前缓存盘适配器(%s)不支持浏览分享", m.backend.Name())
	}
	return p.ListShare(context.Background(), share, subPath)
}

// SaveShare 把分享内某路径手动转存到自己盘指定目录(适配器需实现 ShareSaver)。
func (m *Manager) SaveShare(share ShareRef, srcPath, targetFolder string) error {
	p, ok := m.backend.(ShareSaver)
	if !ok {
		return fmt.Errorf("当前缓存盘适配器(%s)不支持手动转存", m.backend.Name())
	}
	return p.SaveToFolder(context.Background(), share, srcPath, targetFolder)
}

// BackendName 返回当前缓存盘适配器名。
func (m *Manager) BackendName() string { return m.backend.Name() }

// IsHLS 表示当前适配器的播放地址是否为 HLS。
func (m *Manager) IsHLS() bool {
	if p, ok := m.backend.(HLSStreamer); ok {
		return p.IsHLS()
	}
	return false
}

// Status 只读查询缓存状态，**不会触发转存**。
// 用于 HEAD 探测等场景，避免 Emby/Jellyfin 扫描媒体库时把所有资源都转存一遍。
func (m *Manager) Status(resourceID int64) (store.CacheItem, error) {
	return m.store.GetCacheItem(resourceID)
}

// startTransfer 非阻塞触发一次转存，同一资源并发只跑一个。
func (m *Manager) startTransfer(res store.Resource) {
	m.mu.Lock()
	if m.inflight[res.ID] {
		m.mu.Unlock()
		return
	}
	m.inflight[res.ID] = true
	m.mu.Unlock()

	_ = m.store.SetTransferring(res.ID, m.backend.Name())

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.inflight, res.ID)
			m.mu.Unlock()
		}()

		ctx := context.Background()
		share := ShareRef{
			Provider: res.Provider,
			ShareURL: res.ShareURL,
			SharePwd: res.SharePwd,
			FilePath: res.FilePath,
		}

		// 转存阶段只做 copy；copy 成功即就绪（直链在播放时实时取）。
		tr, err := m.backend.Transfer(ctx, share)
		if err != nil {
			log.Printf("转存失败 resource=%d: %v", res.ID, err)
			_ = m.store.SetFailed(res.ID, m.backend.Name(), err.Error())
			return
		}
		_ = m.store.SetReady(res.ID, m.backend.Name(), tr.CachePath, "", tr.Size)
		log.Printf("转存就绪 resource=%d path=%s size=%d", res.ID, tr.CachePath, tr.Size)
	}()
}
