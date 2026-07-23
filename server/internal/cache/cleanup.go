package cache

import (
	"context"
	"log/slog"
	"time"

	"ivideo/server/internal/store"
)

// StartCleanup 启动后台清理循环。
//   - 会话感知(注入了 SessionSource 时)：正在播放/暂停的资源受保护，
//     真正停掉且过了 stopGraceMinutes 宽限期才删 —— 即"停了才删，暂停不删"。
//   - TTL：会话不可用时的主力，或作为"很久没动"的安全网。ttlHours<=0 关闭。
//   - 配额：maxBytes<=0 关闭。总量超限时从最久未看的删起，但跳过正在看的。
func (m *Manager) StartCleanup(intervalMinutes, ttlHours int, maxBytes int64, stopGraceMinutes int) {
	if intervalMinutes <= 0 {
		intervalMinutes = 10
	}
	graceSec := int64(stopGraceMinutes) * 60
	go func() {
		ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.cleanupOnce(ttlHours, maxBytes, graceSec)
		}
	}()
	slog.Info("清理任务已启动", "intervalMinutes", intervalMinutes, "ttlHours", ttlHours,
		"maxBytes", maxBytes, "stopGraceMinutes", stopGraceMinutes, "会话感知", m.sessions != nil)
}

// cleanupOnce 执行一轮清理。
func (m *Manager) cleanupOnce(ttlHours int, maxBytes int64, stopGraceSec int64) {
	items, err := m.store.ListReady() // 已按 last_access 升序（最久未看在前）
	if err != nil {
		slog.Error("清理：读取缓存项失败", "err", err)
		return
	}

	// 会话感知：拿到正在播放/暂停的资源集合（拿不到就退回纯 TTL）。
	var active map[int64]bool
	sessionOK := false
	if m.sessions != nil {
		if a, err := m.sessions.ActiveResourceIDs(); err == nil {
			active, sessionOK = a, true
		} else {
			slog.Warn("清理：读取 Jellyfin 会话失败，本轮退回 TTL", "err", err)
		}
	}

	now := time.Now().Unix()
	ttlCutoff := int64(0)
	if ttlHours > 0 {
		ttlCutoff = now - int64(ttlHours)*3600
	}
	deleted := false

	for _, it := range items {
		// 正在会话中（播放/暂停）→ 保护，重置停止计时。
		if sessionOK && active[it.ResourceID] {
			delete(m.stoppedAt, it.ResourceID)
			continue
		}

		// 停止感知淘汰：不在任何会话里 + 宽限期过了 → 删。
		if sessionOK && stopGraceSec > 0 {
			first := m.stoppedAt[it.ResourceID]
			if first == 0 {
				m.stoppedAt[it.ResourceID] = now
				first = now
			}
			if now-first >= stopGraceSec {
				if m.evict(it) {
					deleted = true
					delete(m.stoppedAt, it.ResourceID)
				}
				continue
			}
		}

		// TTL 兜底。
		if ttlCutoff > 0 && it.LastAccess < ttlCutoff {
			if m.evict(it) {
				deleted = true
			}
		}
	}

	// 配额淘汰：总量超上限时从最久未看的删起，但跳过正在看的。
	if maxBytes > 0 {
		if remaining, err := m.store.ListReady(); err == nil {
			var total int64
			for _, it := range remaining {
				total += it.Size
			}
			for _, it := range remaining {
				if total <= maxBytes {
					break
				}
				if sessionOK && active[it.ResourceID] {
					continue // 正在看的不动
				}
				if m.evict(it) {
					total -= it.Size
					deleted = true
				}
			}
		}
	}

	// 真正释放配额需清空回收站。
	if deleted {
		if err := m.backend.EmptyTrash(context.Background()); err != nil {
			slog.Error("清理：清空回收站失败", "err", err)
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
			slog.Error("清理：删除失败", "resource", it.ResourceID, "path", it.CachePath, "err", err)
			return false
		}
	}
	_ = m.store.MarkCleaned(it.ResourceID)
	slog.Info("清理：已释放", "resource", it.ResourceID, "path", it.CachePath)
	return true
}
