package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resourcesearch"
	"ivideo/server/internal/resp"
)

// SearchResources 从配置的公开来源搜索网盘分享。GET /api/v1/search/resources
func (h *Handler) SearchResources(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if len([]rune(query)) < 2 {
		resp.Fail(c, http.StatusBadRequest, "搜索关键词至少需要 2 个字符")
		return
	}
	refresh := c.Query("refresh") == "1" || strings.EqualFold(c.Query("refresh"), "true")
	snapshot, err := h.discovery.SearchProgressive(
		c.Request.Context(), query, refresh,
		time.Duration(h.cfg.DiscoveryQuickWaitSeconds)*time.Second,
	)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	resp.OK(c, snapshot)
}

// SyncGitHubResources triggers one incremental collection. The collector reads
// only changed repository list files and persists its result before replying.
// POST /api/v1/search/github/resources/sync
func (h *Handler) SyncGitHubResources(c *gin.Context) {
	status, err := h.runGitHubCollection(c.Request.Context())
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	resp.OK(c, gin.H{"discovered": status.SharesAdded + status.SharesKnown, "added": status.SharesAdded, "existing": status.SharesKnown, "status": status})
}

// ListGitHubSources 返回当前接入的 GitHub 资源源列表。
func (h *Handler) ListGitHubSources(c *gin.Context) {
	repositories, err := h.store.ListGitHubRepositories()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]gin.H, 0, len(repositories))
	for _, repository := range repositories {
		items = append(items, gin.H{"id": repository.ID, "name": repository.Repository, "repository": repository.Repository,
			"url": "https://github.com/" + repository.Repository, "parser": repository.Parser,
			"description": "后台增量采集，只有发生变化的资源清单文件才会读取。", "configured": repository.Enabled,
			"lastCollectedAt": repository.LastCollectedAt, "lastError": repository.LastError})
	}
	resp.OK(c, gin.H{"items": items, "status": h.getGitHubCollectionStatus()})
}

// ListGitHubResources 返回 GitHub 资源源解析出的全部资源，支持服务端分页。
func (h *Handler) ListGitHubResources(c *gin.Context) {
	page := 1
	pageSize := 50
	if value := c.Query("page"); value != "" {
		_, _ = fmt.Sscanf(value, "%d", &page)
	}
	if value := c.Query("pageSize"); value != "" {
		_, _ = fmt.Sscanf(value, "%d", &pageSize)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	items, total, err := h.store.ListGitHubObservedShares(page, pageSize)
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseItems := make([]gin.H, 0, len(items))
	for _, item := range items {
		responseItems = append(responseItems, gin.H{"provider": item.Provider, "shareUrl": item.ShareURL, "sharePwd": item.SharePwd,
			"title": item.Title, "resourceType": item.ResourceType, "fileName": item.FileName, "updatedAt": item.UpdatedAt,
			"source": "github-collection", "sourceName": item.Repository, "repository": item.Repository, "path": item.Path, "sourceUrl": item.SourceURL})
	}
	resp.OK(c, gin.H{"items": responseItems, "total": total, "page": page, "pageSize": pageSize, "meta": gin.H{"source": "github-collection"}})
}

// SearchSettings 返回公开搜索来源的配置状态。GET /api/v1/settings/search
func (h *Handler) SearchSettings(c *gin.Context) {
	payload, err := h.searchSettingsPayload()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, payload)
}

func (h *Handler) searchSettingsPayload() (gin.H, error) {
	credential, found, err := h.store.GetCredential("github")
	if err != nil {
		return nil, err
	}
	return gin.H{
		"githubConfigured": found && credential.Token != "", "updatedAt": credential.UpdatedAt,
		"engine": gin.H{
			"name": "ivideo 资源发现引擎", "sources": h.discovery.Status(),
			"cacheMinutes": h.cfg.DiscoveryCacheMinutes, "maxResults": h.cfg.DiscoveryMaxResults,
		},
	}, nil
}

// UpdateSearchSource persists and applies one source plugin switch.
func (h *Handler) UpdateSearchSource(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		resp.Fail(c, http.StatusBadRequest, "enabled 必须是布尔值")
		return
	}
	previous := false
	found := false
	for _, source := range h.discovery.Status() {
		if source.ID == id {
			previous, found = source.Enabled, true
			break
		}
	}
	if !found {
		resp.Fail(c, http.StatusNotFound, "资源来源不存在")
		return
	}
	if err := h.discovery.SetSourceEnabled(id, *req.Enabled); err != nil {
		resp.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.SetSetting(resourcesearch.SourceEnabledSettingKey(id), fmt.Sprintf("%t", *req.Enabled)); err != nil {
		_ = h.discovery.SetSourceEnabled(id, previous)
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	payload, err := h.searchSettingsPayload()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, payload)
}

// SaveGitHubToken 校验并保存 GitHub 只读访问令牌。POST /api/v1/settings/search/github
func (h *Handler) SaveGitHubToken(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Token) == "" {
		resp.Fail(c, http.StatusBadRequest, "Token 不能为空")
		return
	}
	token := strings.TrimSpace(req.Token)
	if err := resourcesearch.NewGitHub(token).CheckToken(c.Request.Context()); err != nil {
		resp.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.SetCredential("github", token, "public_code_search"); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	payload, err := h.searchSettingsPayload()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, payload)
}

// DeleteGitHubToken 清除 GitHub 搜索令牌。DELETE /api/v1/settings/search/github
func (h *Handler) DeleteGitHubToken(c *gin.Context) {
	if err := h.store.SetCredential("github", "", ""); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	payload, err := h.searchSettingsPayload()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, payload)
}
