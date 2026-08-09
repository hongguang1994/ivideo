package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const githubCollectInterval = 6 * time.Hour

type githubCollectionStatus struct {
	Running      bool   `json:"running"`
	Repositories int    `json:"repositories"`
	FilesUpdated int    `json:"filesUpdated"`
	SharesAdded  int    `json:"sharesAdded"`
	SharesKnown  int    `json:"sharesKnown"`
	LastError    string `json:"lastError"`
	StartedAt    int64  `json:"startedAt"`
	FinishedAt   int64  `json:"finishedAt"`
}

func (h *Handler) getGitHubCollectionStatus() githubCollectionStatus {
	h.githubStatusMu.RLock()
	defer h.githubStatusMu.RUnlock()
	return h.githubStatus
}

func (h *Handler) setGitHubCollectionStatus(status githubCollectionStatus) {
	h.githubStatusMu.Lock()
	h.githubStatus = status
	h.githubStatusMu.Unlock()
}

// StartGitHubCollector maintains approved GitHub repositories in the local
// index. A short delayed initial run keeps application startup responsive.
func (h *Handler) StartGitHubCollector() {
	if h.githubCollector == nil {
		return
	}
	go func() {
		time.Sleep(5 * time.Second)
		h.runGitHubCollection(context.Background())
		ticker := time.NewTicker(githubCollectInterval)
		defer ticker.Stop()
		for range ticker.C {
			h.runGitHubCollection(context.Background())
		}
	}()
}

func (h *Handler) runGitHubCollection(ctx context.Context) (githubCollectionStatus, error) {
	if h.githubCollector == nil {
		return githubCollectionStatus{}, fmt.Errorf("GitHub 采集模块未启用")
	}
	if !h.githubRunMu.TryLock() {
		return h.githubStatus, fmt.Errorf("GitHub 采集正在进行")
	}
	defer h.githubRunMu.Unlock()
	status := h.getGitHubCollectionStatus()
	status.Running, status.StartedAt, status.LastError = true, time.Now().Unix(), ""
	h.setGitHubCollectionStatus(status)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	results, err := h.githubCollector.CollectApproved(ctx)
	status.Running, status.FinishedAt = false, time.Now().Unix()
	if err != nil {
		status.LastError = err.Error()
		h.setGitHubCollectionStatus(status)
		slog.Warn("GitHub 资源采集失败", "err", err)
		return status, err
	}
	status.Repositories, status.FilesUpdated, status.SharesAdded, status.SharesKnown = len(results), 0, 0, 0
	for _, result := range results {
		status.FilesUpdated += result.FilesUpdated
		status.SharesAdded += result.SharesAdded
		status.SharesKnown += result.SharesKnown
	}
	h.setGitHubCollectionStatus(status)
	slog.Info("GitHub 资源采集完成", "repositories", status.Repositories, "files", status.FilesUpdated, "added", status.SharesAdded)
	return status, nil
}
