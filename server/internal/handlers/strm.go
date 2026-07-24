package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resp"

	"ivideo/server/internal/strm"
)

// generateStrm 重建 strm 媒体库。全量但幂等：内容没变的文件不重写、不动 mtime，
// 所以可以随便重复调用（导入完成后自动调、定时兜底调）。
func (h *Handler) generateStrm() (strm.Result, error) {
	g := strm.New(h.store, h.cfg.MediaDir, h.cfg.SiteURL, h.cfg.StrmMode, APIPrefix)
	return g.Generate()
}

// autoGenerateStrm 在资源库变动后自动重建 strm，失败只记日志、不影响主流程。
// 媒体库真的变了（有新写入或清理）才顺手让 Jellyfin 扫库 —— 新剧集才会出现。
func (h *Handler) autoGenerateStrm(reason string) {
	res, err := h.generateStrm()
	if err != nil {
		slog.Error("自动生成 strm 失败", "reason", reason, "err", err)
		return
	}
	slog.Info("自动生成 strm", "reason", reason, "total", res.Total,
		"written", res.Written, "unchanged", res.Unchanged, "removed", res.Removed)

	if !res.Changed() || h.jf == nil {
		return
	}
	if err := h.jf.RefreshLibrary(); err != nil {
		slog.Warn("通知 Jellyfin 扫库失败", "err", err)
		return
	}
	slog.Info("已通知 Jellyfin 扫库", "reason", reason)
}

// StartAutoStrm 启动 strm 自动维护：启动时先生成一次，之后每 intervalMinutes 分钟兜底重建。
// 兜底是为了覆盖「不经导入接口的资源变动」（直接改库、手动删资源等）。
// intervalMinutes <= 0 表示只在启动时生成一次，不开定时。
func (h *Handler) StartAutoStrm(intervalMinutes int) {
	h.autoGenerateStrm("startup")
	if intervalMinutes <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
		defer t.Stop()
		for range t.C {
			h.autoGenerateStrm("periodic")
		}
	}()
}

// GenerateStrm 全量重建 strm 媒体库(给 Emby/Jellyfin 扫描)。
// POST /api/strm/generate
func (h *Handler) GenerateStrm(c *gin.Context) {
	res, err := h.generateStrm()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{
		"total":     res.Total,
		"written":   res.Written,
		"unchanged": res.Unchanged,
		"removed":   res.Removed,
		"errors":    res.Errors,
		"mediaDir":  h.cfg.MediaDir,
		"siteUrl":   h.cfg.SiteURL,
		"mode":      h.cfg.StrmMode,
	})
}
