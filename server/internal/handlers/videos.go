package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Video 是返回给前端的视频/目录条目。
type Video struct {
	Name     string `json:"name"`
	Path     string `json:"path"`     // 相对视频根目录的路径
	IsDir    bool   `json:"isDir"`    // 目录用于层级浏览
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Thumb    string `json:"thumb"`
	StreamURL string `json:"streamUrl,omitempty"` // 播放地址（仅文件）
}

// ListVideos 列出某目录下的子目录与视频文件。
// GET /api/videos?path=/some/dir
func (h *Handler) ListVideos(c *gin.Context) {
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
			Name:     it.Name,
			Path:     childRel,
			IsDir:    it.IsDir,
			Size:     it.Size,
			Modified: it.Modified,
			Thumb:    it.Thumb,
		}
		if !it.IsDir {
			v.StreamURL = "/api/stream?path=" + childRel
		}
		out = append(out, v)
	}

	c.JSON(http.StatusOK, gin.H{"path": rel, "items": out})
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
