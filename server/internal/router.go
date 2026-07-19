package internal

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/config"
	"ivideo/server/internal/handlers"
	"ivideo/server/internal/openlist"
)

// NewRouter 组装 Gin 路由。
func NewRouter(cfg config.Config) *gin.Engine {
	ol := openlist.New(cfg.OpenListBaseURL, cfg.OpenListUsername, cfg.OpenListPassword)
	h := handlers.New(cfg, ol)

	r := gin.Default()

	// 健康检查
	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api")
	{
		api.GET("/videos", h.ListVideos)
		api.GET("/stream", h.Stream)
	}

	return r
}
