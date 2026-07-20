package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/strm"
)

// GenerateStrm 全量重建 strm 媒体库(给 Emby/Jellyfin 扫描)。
// POST /api/strm/generate
func (h *Handler) GenerateStrm(c *gin.Context) {
	g := strm.New(h.store, h.cfg.MediaDir, h.cfg.SiteURL, h.cfg.StrmMode)
	res, err := g.Generate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total":    res.Total,
		"written":  res.Written,
		"removed":  res.Removed,
		"errors":   res.Errors,
		"mediaDir": h.cfg.MediaDir,
		"siteUrl":  h.cfg.SiteURL,
		"mode":     h.cfg.StrmMode,
	})
}
