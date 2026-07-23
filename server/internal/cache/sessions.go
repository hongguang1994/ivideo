package cache

import (
	"os"
	"strconv"
	"strings"

	"ivideo/server/internal/jellyfin"
)

// SessionSource 提供当前"正在会话中(播放/暂停)"的资源 ID 集合。
// 清理任务据此保护正在看的资源，只删真正停掉的。
type SessionSource interface {
	ActiveResourceIDs() (map[int64]bool, error)
}

// JellyfinSessions 从 Jellyfin 会话解析出正在播放/暂停的资源 ID。
type JellyfinSessions struct {
	jf       *jellyfin.Client
	mediaDir string
}

// NewJellyfinSessions 创建 Jellyfin 会话源。mediaDir 用于读 strm 文件反查资源 ID。
func NewJellyfinSessions(jf *jellyfin.Client, mediaDir string) *JellyfinSessions {
	return &JellyfinSessions{jf: jf, mediaDir: mediaDir}
}

// ActiveResourceIDs 拉 Jellyfin 会话，映射成正在使用(播放/暂停)的资源 ID 集合。
func (j *JellyfinSessions) ActiveResourceIDs() (map[int64]bool, error) {
	items, err := j.jf.NowPlaying()
	if err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(items))
	for _, it := range items {
		if id := resolveResourceID(it); id > 0 {
			out[id] = true
		}
	}
	return out, nil
}

// resolveResourceID 把一个正在播放的条目映射回 ivideo 资源 ID：
//  1. 媒体源地址里直接含 /file/{id} 或 /hls/{id}
//  2. 否则读 .strm 文件内容(里面就是那条 ivideo 地址)解析
func resolveResourceID(it jellyfin.PlayingItem) int64 {
	if id := parseFileID(it.MediaURL); id > 0 {
		return id
	}
	if strings.HasSuffix(strings.ToLower(it.Path), ".strm") {
		if b, err := os.ReadFile(it.Path); err == nil {
			if id := parseFileID(string(b)); id > 0 {
				return id
			}
		}
	}
	return 0
}

// parseFileID 从含 "/file/{id}" 或 "/hls/{id}" 的字符串里取资源 ID。
func parseFileID(s string) int64 {
	for _, marker := range []string{"/file/", "/hls/"} {
		i := strings.Index(s, marker)
		if i < 0 {
			continue
		}
		rest := s[i+len(marker):]
		n := 0
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		if n > 0 {
			if id, err := strconv.ParseInt(rest[:n], 10, 64); err == nil {
				return id
			}
		}
	}
	return 0
}
