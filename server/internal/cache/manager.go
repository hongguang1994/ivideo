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
	store    *store.Store
	backend  CacheBackend
	cacheDir string

	mu       sync.Mutex
	inflight map[int64]bool // 正在转存的资源 ID，用于去重
}

// NewManager 创建缓存管理器。
func NewManager(st *store.Store, backend CacheBackend, cacheDir string) *Manager {
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
	if item.Status == store.StatusReady && item.DirectURL != "" {
		_ = m.store.TouchAccess(resourceID)
		return item, nil
	}

	m.startTransfer(res)

	// 返回触发后的最新状态（大概率是 transferring）。
	item, _ = m.store.GetCacheItem(resourceID)
	return item, nil
}

// StreamURL 返回可代理播放的直链；未就绪则返回错误（交由上层提示“转存中”）。
func (m *Manager) StreamURL(resourceID int64) (string, error) {
	item, err := m.EnsureReady(resourceID)
	if err != nil {
		return "", err
	}
	if item.Status != store.StatusReady || item.DirectURL == "" {
		return "", fmt.Errorf("资源尚未就绪（%s）", item.Status)
	}
	return item.DirectURL, nil
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

		tr, err := m.backend.Transfer(ctx, share)
		if err != nil {
			log.Printf("转存失败 resource=%d: %v", res.ID, err)
			_ = m.store.SetFailed(res.ID, m.backend.Name(), err.Error())
			return
		}
		url, err := m.backend.DirectURL(ctx, tr.CachePath)
		if err != nil {
			log.Printf("取直链失败 resource=%d: %v", res.ID, err)
			_ = m.store.SetFailed(res.ID, m.backend.Name(), err.Error())
			return
		}
		_ = m.store.SetReady(res.ID, m.backend.Name(), tr.CachePath, url, tr.Size)
		log.Printf("转存就绪 resource=%d path=%s size=%d", res.ID, tr.CachePath, tr.Size)
	}()
}
