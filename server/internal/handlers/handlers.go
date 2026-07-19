package handlers

import (
	"path"
	"strings"

	"ivideo/server/internal/config"
	"ivideo/server/internal/openlist"
)

// Handler 聚合请求处理所需的依赖。
type Handler struct {
	cfg config.Config
	ol  *openlist.Client
}

// New 创建 Handler。
func New(cfg config.Config, ol *openlist.Client) *Handler {
	return &Handler{cfg: cfg, ol: ol}
}

// isVideo 判断文件名是否为受支持的视频格式。
func (h *Handler) isVideo(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	for _, e := range h.cfg.VideoExts {
		if e == ext {
			return true
		}
	}
	return false
}

// resolve 把前端传入的相对路径拼到配置的视频根目录下。
func (h *Handler) resolve(p string) string {
	if p == "" {
		p = "/"
	}
	return path.Join("/", h.cfg.OpenListRoot, p)
}
