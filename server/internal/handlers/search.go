package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resourcesearch"
	"ivideo/server/internal/resp"
	"ivideo/server/internal/store"
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

// SyncGitHubResources 将已接入资源源解析出的分享去重后写入分享库。
// POST /api/v1/search/github/resources/sync
func (h *Handler) SyncGitHubResources(c *gin.Context) {
	credential, found, err := h.store.GetCredential("github")
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if !found || credential.Token == "" {
		resp.Fail(c, http.StatusBadRequest, "请先在设置中配置 GitHub Token")
		return
	}
	items, _, err := resourcesearch.NewAliyunPanShare(credential.Token).List(c.Request.Context())
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	shares := make([]store.Share, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		key := batchShareKey(item.Provider, item.ShareURL)
		if item.ShareURL == "" || item.Provider == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		shares = append(shares, store.Share{
			Provider: item.Provider, ShareURL: item.ShareURL, SharePwd: item.SharePwd,
			ShareID: extractShareID(item.ShareURL), Title: item.Title,
			Category: item.ResourceType, Status: "unknown",
		})
	}
	added, existing, err := h.store.SyncShares(shares)
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{"discovered": len(shares), "added": added, "existing": existing})
}

// ListGitHubSources 返回当前接入的 GitHub 资源源列表。
func (h *Handler) ListGitHubSources(c *gin.Context) {
	credential, found, err := h.store.GetCredential("github")
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	configured := found && credential.Token != ""
	resp.OK(c, gin.H{"items": []gin.H{
		{
			"id":          "acoooder/aliyunpanshare",
			"name":        "阿里云盘分享库",
			"repository":  "acoooder/aliyunpanshare",
			"url":         "https://github.com/acoooder/aliyunpanshare",
			"parser":      "Markdown 资源表格",
			"description": "解析资源名称、类型、文件名、分享链接和更新时间。",
			"configured":  configured,
		},
		{
			"id":          "github-code-search",
			"name":        "GitHub 全网公开搜索",
			"repository":  "所有公开仓库",
			"url":         "https://github.com/search",
			"parser":      "分享链接识别",
			"description": "搜索所有能被 GitHub Code Search 找到的公开仓库内容。",
			"configured":  configured,
		},
	}})
}

// ListGitHubResources 返回 GitHub 资源源解析出的全部资源，支持服务端分页。
func (h *Handler) ListGitHubResources(c *gin.Context) {
	credential, found, err := h.store.GetCredential("github")
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if !found || credential.Token == "" {
		resp.Fail(c, http.StatusBadRequest, "请先在设置中配置 GitHub Token")
		return
	}
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
	items, meta, err := resourcesearch.NewAliyunPanShare(credential.Token).List(c.Request.Context())
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	total := len(items)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	resp.OK(c, gin.H{"items": items[start:end], "total": total, "page": page, "pageSize": pageSize, "meta": meta})
}

// SearchSettings 返回公开搜索来源的配置状态。GET /api/v1/settings/search
func (h *Handler) SearchSettings(c *gin.Context) {
	credential, found, err := h.store.GetCredential("github")
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{
		"githubConfigured": found && credential.Token != "", "updatedAt": credential.UpdatedAt,
		"engine": gin.H{
			"name": "ivideo 资源发现引擎", "sources": h.discovery.Status(),
			"cacheMinutes": h.cfg.DiscoveryCacheMinutes, "maxResults": h.cfg.DiscoveryMaxResults,
		},
	})
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
	resp.OK(c, gin.H{"githubConfigured": true})
}

// DeleteGitHubToken 清除 GitHub 搜索令牌。DELETE /api/v1/settings/search/github
func (h *Handler) DeleteGitHubToken(c *gin.Context) {
	if err := h.store.SetCredential("github", "", ""); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{"githubConfigured": false})
}
