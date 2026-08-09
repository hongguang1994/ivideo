package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/resp"
	"ivideo/server/internal/store"
)

const importScheduleSetting = "import_schedule"

type importSchedule struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"intervalMinutes"`
}

type importTaskStatus struct {
	Running    bool   `json:"running"`
	Total      int    `json:"total"`
	Processed  int    `json:"processed"`
	Imported   int    `json:"imported"`
	Skipped    int    `json:"skipped"`
	Failed     int    `json:"failed"`
	Current    string `json:"current"`
	LastError  string `json:"lastError"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
}

func defaultImportSchedule() importSchedule { return importSchedule{IntervalMinutes: 360} }

func (h *Handler) loadImportSchedule() importSchedule {
	schedule := defaultImportSchedule()
	raw, found, err := h.store.GetSetting(importScheduleSetting)
	if err != nil || !found || raw == "" {
		return schedule
	}
	if json.Unmarshal([]byte(raw), &schedule) != nil {
		return defaultImportSchedule()
	}
	if schedule.IntervalMinutes < 5 {
		schedule.IntervalMinutes = 5
	}
	return schedule
}

func (h *Handler) saveImportSchedule(schedule importSchedule) error {
	b, err := json.Marshal(schedule)
	if err != nil {
		return err
	}
	return h.store.SetSetting(importScheduleSetting, string(b))
}

func (h *Handler) getImportStatus() importTaskStatus {
	h.importStatusMu.RLock()
	defer h.importStatusMu.RUnlock()
	return h.importStatus
}

func (h *Handler) setImportStatus(status importTaskStatus) {
	h.importStatusMu.Lock()
	h.importStatus = status
	h.importStatusMu.Unlock()
}

// ImportSettings 返回定时导入配置和当前任务进度。
func (h *Handler) ImportSettings(c *gin.Context) {
	resp.OK(c, gin.H{"schedule": h.loadImportSchedule(), "status": h.getImportStatus()})
}

// SaveImportSettings 保存定时导入配置。周期最短 5 分钟，默认关闭。
func (h *Handler) SaveImportSettings(c *gin.Context) {
	var req importSchedule
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, http.StatusBadRequest, "导入设置格式不正确")
		return
	}
	if req.IntervalMinutes < 5 || req.IntervalMinutes > 10080 {
		resp.Fail(c, http.StatusBadRequest, "导入周期必须在 5 分钟到 7 天之间")
		return
	}
	if err := h.saveImportSchedule(req); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{"schedule": req, "status": h.getImportStatus()})
}

// RunImportTask 手动启动一次后台批量导入。
func (h *Handler) RunImportTask(c *gin.Context) {
	if !h.startImportTask("manual") {
		resp.Fail(c, http.StatusConflict, "导入任务正在运行")
		return
	}
	resp.OK(c, gin.H{"started": true, "status": h.getImportStatus()})
}

// ImportTaskStatus 返回后台导入的实时进度。
func (h *Handler) ImportTaskStatus(c *gin.Context) { resp.OK(c, h.getImportStatus()) }

// StartImportScheduler 启动定时导入轮询。配置存数据库，因此修改设置后无需重启服务。
func (h *Handler) StartImportScheduler() {
	go func() {
		var lastRun int64
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			schedule := h.loadImportSchedule()
			if !schedule.Enabled || h.getImportStatus().Running {
				continue
			}
			now := time.Now().Unix()
			if lastRun != 0 && now-lastRun < int64(schedule.IntervalMinutes)*60 {
				continue
			}
			lastRun = now
			h.startImportTask("scheduled")
		}
	}()
}

func (h *Handler) startImportTask(reason string) bool {
	if !h.importRunMu.TryLock() {
		return false
	}
	h.setImportStatus(importTaskStatus{Running: true, StartedAt: time.Now().Unix()})
	go func() {
		defer h.importRunMu.Unlock()
		h.runImportTask(reason)
	}()
	return true
}

func (h *Handler) runImportTask(reason string) {
	shares, err := h.store.ListShares()
	if err != nil {
		h.setImportStatus(importTaskStatus{LastError: err.Error(), FinishedAt: time.Now().Unix()})
		return
	}
	// 每轮最多处理 20 个分享，给网盘接口留出空间；通过轮转游标覆盖剩余分享。
	if len(shares) > 20 {
		start := h.importCursor % len(shares)
		batch := make([]store.Share, 0, 20)
		for i := 0; i < 20; i++ {
			batch = append(batch, shares[(start+i)%len(shares)])
		}
		h.importCursor = (start + len(batch)) % len(shares)
		shares = batch
	} else {
		h.importCursor = 0
	}
	status := h.getImportStatus()
	status.Total = len(shares)
	h.setImportStatus(status)
	for _, share := range shares {
		status = h.getImportStatus()
		status.Current = share.Title
		if status.Current == "" {
			status.Current = share.ShareURL
		}
		h.setImportStatus(status)
		if share.Status == "invalid" {
			status.Failed++
			status.LastError = "分享已标记为失效"
			status.Processed++
			h.setImportStatus(status)
			continue
		}
		added, skipped, errs := h.importShareResources(context.Background(), cache.ShareRef{Provider: share.Provider, ShareURL: share.ShareURL, SharePwd: share.SharePwd}, reason, true)
		status = h.getImportStatus()
		status.Imported += added
		status.Skipped += skipped
		status.Failed += len(errs)
		if len(errs) > 0 {
			status.LastError = strings.Join(errs, "; ")
		}
		status.Processed++
		h.setImportStatus(status)
		time.Sleep(500 * time.Millisecond)
	}
	status = h.getImportStatus()
	status.Running = false
	status.Current = ""
	status.FinishedAt = time.Now().Unix()
	h.setImportStatus(status)
	if status.Imported > 0 {
		if err := h.importer.CompleteBatch(context.Background(), reason, status.Imported); err != nil {
			status.LastError = err.Error()
			h.setImportStatus(status)
		}
	}
	slog.Info("批量导入完成", "reason", reason, "processed", status.Processed, "imported", status.Imported, "skipped", status.Skipped, "failed", status.Failed)
}
