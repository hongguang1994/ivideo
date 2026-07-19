package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Video 是返回给前端的视频/目录条目，兼容 OpenList 与 Jellyfin 两种来源。
type Video struct {
	Source    string `json:"source"`              // "openlist" 或 "jellyfin"
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`      // OpenList：相对视频根目录的路径
	ID        string `json:"id,omitempty"`        // Jellyfin：条目 ID
	IsDir     bool   `json:"isDir"`               // OpenList 目录用于层级浏览
	Size      int64  `json:"size,omitempty"`
	Modified  string `json:"modified,omitempty"`
	Poster    string `json:"poster,omitempty"`    // 海报地址（经后端代理）
	Overview  string `json:"overview,omitempty"`  // 简介（Jellyfin）
	Year      int    `json:"year,omitempty"`      // 年份（Jellyfin）
	StreamURL string `json:"streamUrl,omitempty"` // 播放地址（仅文件/条目）
}

// ListVideos 按来源列出视频。
// GET /api/videos?source=openlist&path=/dir
// GET /api/videos?source=jellyfin
func (h *Handler) ListVideos(c *gin.Context) {
	switch c.DefaultQuery("source", "openlist") {
	case "jellyfin":
		h.listJellyfin(c)
	default:
		h.listOpenList(c)
	}
}

// listOpenList 列出某目录下的子目录与视频文件。
func (h *Handler) listOpenList(c *gin.Context) {
	rel := c.Query("path")
	items, err := h.ol.List(h.resolve(rel))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	out := make([]Video, 0, len(items))
	for _, it := range items {
		if !it.IsDir && !h.isVideo(it.Name) {
			continue // 跳过非视频文件
		}
		childRel := joinRel(rel, it.Name)
		v := Video{
			Source:   "openlist",
			Name:     it.Name,
			Path:     childRel,
			IsDir:    it.IsDir,
			Size:     it.Size,
			Modified: it.Modified,
			Poster:   it.Thumb,
		}
		if !it.IsDir {
			v.StreamURL = "/api/stream?source=openlist&path=" + childRel
		}
		out = append(out, v)
	}

	c.JSON(http.StatusOK, gin.H{"source": "openlist", "path": rel, "items": out})
}

// listJellyfin 列出 Jellyfin 片库中的影片。
func (h *Handler) listJellyfin(c *gin.Context) {
	if h.jf == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 Jellyfin"})
		return
	}
	itemTypes := c.DefaultQuery("types", "Movie")
	items, err := h.jf.Items(itemTypes)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	out := make([]Video, 0, len(items))
	for _, it := range items {
		v := Video{
			Source:    "jellyfin",
			Name:      it.Name,
			ID:        it.ID,
			Overview:  it.Overview,
			Year:      it.ProductionYear,
			StreamURL: "/api/stream?source=jellyfin&id=" + it.ID,
		}
		// 有主海报才给出代理地址
		if _, ok := it.ImageTags["Primary"]; ok {
			v.Poster = fmt.Sprintf("/api/image?source=jellyfin&id=%s", it.ID)
		}
		out = append(out, v)
	}

	c.JSON(http.StatusOK, gin.H{"source": "jellyfin", "items": out})
}

// joinRel 拼接相对路径，始终以 / 开头。
func joinRel(dir, name string) string {
	if dir == "" || dir == "/" {
		return "/" + name
	}
	if dir[len(dir)-1] == '/' {
		return dir + name
	}
	return dir + "/" + name
}
