package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resp"

	"ivideo/server/internal/strm"
)

// GenerateStrm 全量重建 strm 媒体库(给 Emby/Jellyfin 扫描)。
// POST /api/strm/generate
func (h *Handler) GenerateStrm(c *gin.Context) {
	g := strm.New(h.store, h.cfg.MediaDir, h.cfg.SiteURL, h.cfg.StrmMode, APIPrefix)
	res, err := g.Generate()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{
		"total":    res.Total,
		"written":  res.Written,
		"removed":  res.Removed,
		"errors":   res.Errors,
		"mediaDir": h.cfg.MediaDir,
		"siteUrl":  h.cfg.SiteURL,
		"mode":     h.cfg.StrmMode,
	})
}
