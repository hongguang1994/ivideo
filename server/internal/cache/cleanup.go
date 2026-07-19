package cache

import (
	"context"
	"log"
	"time"

	"ivideo/server/internal/store"
)

// StartCleanup 启动后台清理循环：按 TTL 和配额上限淘汰缓存，并清空回收站。
// ttlHours<=0 关闭 TTL 淘汰；maxBytes<=0 关闭配额淘汰。
func (m *Manager) StartCleanup(intervalMinutes, ttlHours int, maxBytes int64) {
	if intervalMinutes <= 0 {
		intervalMinutes = 10
	}
	go func() {
		ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.cleanupOnce(ttlHours, maxBytes)
		}
	}()
	log.Printf("清理任务已启动：每 %d 分钟，TTL=%dh，上限=%d 字节", intervalMinutes, ttlHours, maxBytes)
}

// cleanupOnce 执行一轮清理。
func (m *Manager) cleanupOnce(ttlHours int, maxBytes int64) {
	items, err := m.store.ListReady() // 已按 last_access 升序（最久未看在前）
	if err != nil {
		log.Printf("清理：读取缓存项失败: %v", err)
		return
	}

	deleted := false

	// 1) TTL 淘汰：太久没看的直接删。
	if ttlHours > 0 {
		cutoff := time.Now().Add(-time.Duration(ttlHours) * time.Hour).Unix()
		for _, it := range items {
			if it.LastAccess < cutoff {
				if m.evict(it) {
					deleted = true
				}
			}
		}
	}

	// 2) 配额淘汰：总量超上限时，从最久未看的开始删，直到降到上限内。
	if maxBytes > 0 {
		remaining, err := m.store.ListReady()
		if err == nil {
			var total int64
			for _, it := range remaining {
				total += it.Size
			}
			for _, it := range remaining {
				if total <= maxBytes {
					break
				}
				if m.evict(it) {
					total -= it.Size
					deleted = true
				}
			}
		}
	}

	// 3) 真正释放配额需清空回收站。
	if deleted {
		if err := m.backend.EmptyTrash(context.Background()); err != nil {
			log.Printf("清理：清空回收站失败: %v", err)
		}
	}
}

// evict 删除单个缓存文件并在库中标记为已清理。
func (m *Manager) evict(it store.CacheItem) bool {
	// 正在转存的不动。
	m.mu.Lock()
	busy := m.inflight[it.ResourceID]
	m.mu.Unlock()
	if busy {
		return false
	}

	if it.CachePath != "" {
		if err := m.backend.Delete(context.Background(), it.CachePath); err != nil {
			log.Printf("清理：删除失败 resource=%d path=%s: %v", it.ResourceID, it.CachePath, err)
			return false
		}
	}
	_ = m.store.MarkCleaned(it.ResourceID)
	log.Printf("清理：已释放 resource=%d path=%s", it.ResourceID, it.CachePath)
	return true
}
