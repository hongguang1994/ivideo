package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resp"
)

// autoGenerateStrm 在资源库变动后自动重建 strm，失败只记日志、不影响主流程。
// 媒体库真的变了（有新写入或清理）才顺手让 Jellyfin 扫库 —— 新剧集才会出现。
func (h *Handler) autoGenerateStrm(reason string) {
	res, err := h.workflow.Publish(context.Background(), reason)
	if err != nil {
		slog.Error("自动生成 strm 失败", "reason", reason, "err", err)
		return
	}
	slog.Info("自动生成 strm", "reason", reason, "total", res.Total,
		"written", res.Written, "unchanged", res.Unchanged,
		"deduplicated", res.Deduplicated, "conflicts", res.Conflicts, "removed", res.Removed)

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
	res, err := h.workflow.Publish(context.Background(), "manual")
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{
		"total":        res.Total,
		"written":      res.Written,
		"unchanged":    res.Unchanged,
		"deduplicated": res.Deduplicated,
		"conflicts":    res.Conflicts,
		"removed":      res.Removed,
		"errors":       res.Errors,
		"mediaDir":     h.cfg.MediaDir,
		"siteUrl":      h.cfg.SiteURL,
		"mode":         h.cfg.StrmMode,
	})
}
