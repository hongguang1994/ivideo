package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/resp"
	"ivideo/server/internal/store"
)

const shareCheckBatchSize = 20

// checkShare 只请求分享根目录，确认分享仍可访问，同时记录基本统计。
func (h *Handler) checkShare(sh store.Share) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entries, err := h.cache.ListShareContext(ctx, cache.ShareRef{
		Provider: sh.Provider, ShareURL: sh.ShareURL, SharePwd: sh.SharePwd,
	}, "")
	now := time.Now().Unix()
	sh.LastCheckedAt = now
	if err != nil {
		// 限流、超时和上游暂时不可用不能判定分享失效，保留原状态，等待下次检查。
		if isTransientShareCheckError(err) {
			sh.LastCheckedAt = now
			_ = h.store.UpdateShare(sh)
			return err
		}
		sh.Status = "invalid"
		sh.FileCount = 0
		sh.TotalSize = 0
		if updateErr := h.store.UpdateShare(sh); updateErr != nil {
			return updateErr
		}
		return err
	}
	sh.Status = "valid"
	sh.FileCount = len(entries)
	sh.TotalSize = 0
	for _, entry := range entries {
		if !entry.IsDir && entry.Size > 0 {
			sh.TotalSize += entry.Size
		}
	}
	return h.store.UpdateShare(sh)
}

func (h *Handler) checkAllShares() (checked, valid, invalid int) {
	h.shareCheckMu.Lock()
	defer h.shareCheckMu.Unlock()
	shares, err := h.store.ListShares()
	if err != nil {
		slog.Error("读取分享库失败", "err", err)
		return 0, 0, 0
	}
	// 分批、按最久未检查优先，避免一次同步数千条分享触发网盘限流。
	sort.SliceStable(shares, func(i, j int) bool {
		if shares[i].LastCheckedAt == shares[j].LastCheckedAt {
			return shares[i].CreatedAt < shares[j].CreatedAt
		}
		return shares[i].LastCheckedAt < shares[j].LastCheckedAt
	})
	if len(shares) > shareCheckBatchSize {
		shares = shares[:shareCheckBatchSize]
	}
	for _, sh := range shares {
		checked++
		if err := h.checkShare(sh); err != nil {
			invalid++
			slog.Warn("分享可用性检查失败", "id", sh.ID, "provider", sh.Provider, "url", sh.ShareURL, "err", err)
		} else {
			valid++
		}
		// 给网盘接口留出间隔，避免健康检查挤占用户的浏览/播放请求额度。
		time.Sleep(500 * time.Millisecond)
	}
	slog.Info("分享可用性检查完成", "checked", checked, "valid", valid, "invalid", invalid)
	return checked, valid, invalid
}

func isTransientShareCheckError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"429", "too many requests", "timeout", "deadline exceeded", "temporarily", "bad gateway", "502", "503", "504"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// StartShareChecks 启动分享健康检查。首次检查延迟到服务启动后，避免阻塞接口启动。
func (h *Handler) StartShareChecks(intervalMinutes int) {
	if intervalMinutes <= 0 {
		return
	}
	go func() {
		time.Sleep(10 * time.Second)
		h.checkAllShares()
		ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			h.checkAllShares()
		}
	}()
}

// CheckAllShares 手动检查全部分享。POST /api/v1/shares/check
func (h *Handler) CheckAllShares(c *gin.Context) {
	go h.checkAllShares()
	resp.OK(c, gin.H{"started": true, "message": "检查已在后台开始，请稍后刷新分享库查看状态"})
}

// CheckOneShare 手动检查一个分享。POST /api/v1/shares/:id/check
func (h *Handler) CheckOneShare(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	sh, err := h.store.GetShare(id)
	if err != nil {
		resp.Fail(c, http.StatusNotFound, "分享不存在")
		return
	}
	checkErr := h.checkShare(sh)
	if checkErr != nil {
		status := "invalid"
		if isTransientShareCheckError(checkErr) {
			status = "unknown"
		}
		resp.OK(c, gin.H{"status": status, "message": strings.TrimSpace(checkErr.Error())})
		return
	}
	resp.OK(c, gin.H{"status": "valid"})
}
