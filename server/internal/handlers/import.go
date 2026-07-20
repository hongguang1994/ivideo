package handlers

import (
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/store"
)

// 批量导入的安全上限，避免一个巨大分享把库刷爆。
const (
	importMaxDepth = 4
	importMaxFiles = 500
)

// BrowseShare 列出分享内某目录，供前端挑选要导入的子目录。
// GET /api/share/browse?shareUrl=...&sharePwd=...&path=/子目录
func (h *Handler) BrowseShare(c *gin.Context) {
	shareURL := c.Query("shareUrl")
	if shareURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 shareUrl"})
		return
	}
	entries, err := h.cache.ListShare(cache.ShareRef{
		Provider: c.DefaultQuery("provider", "aliyun"),
		ShareURL: shareURL,
		SharePwd: c.Query("sharePwd"),
	}, c.Query("path"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": entries})
}

// SaveShareItem 把分享内某个文件/文件夹手动转存到自己盘指定目录(默认 ivideo)。
// POST /api/share/save
// body: {shareUrl, sharePwd?, path, targetFolder?, provider?}
func (h *Handler) SaveShareItem(c *gin.Context) {
	var req struct {
		ShareURL     string `json:"shareUrl"`
		SharePwd     string `json:"sharePwd"`
		Path         string `json:"path"`
		TargetFolder string `json:"targetFolder"`
		Provider     string `json:"provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ShareURL == "" || req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 shareUrl 或 path"})
		return
	}
	if req.Provider == "" {
		req.Provider = "aliyun"
	}
	if req.TargetFolder == "" {
		req.TargetFolder = "ivideo"
	}
	err := h.cache.SaveShare(cache.ShareRef{
		Provider: req.Provider,
		ShareURL: req.ShareURL,
		SharePwd: req.SharePwd,
	}, req.Path, req.TargetFolder)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "targetFolder": req.TargetFolder})
}

// ImportShare 递归遍历一个分享，为其中每个视频文件建一条资源。
// POST /api/resources/import
// body: {shareUrl, sharePwd?, path?, provider?}
func (h *Handler) ImportShare(c *gin.Context) {
	var req struct {
		ShareURL string `json:"shareUrl"`
		SharePwd string `json:"sharePwd"`
		Path     string `json:"path"`
		Provider string `json:"provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ShareURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 shareUrl"})
		return
	}
	if req.Provider == "" {
		req.Provider = "aliyun"
	}
	share := cache.ShareRef{Provider: req.Provider, ShareURL: req.ShareURL, SharePwd: req.SharePwd}

	// 已有资源的 (shareUrl, filePath) 集合，用于去重。
	existing := map[string]bool{}
	if list, err := h.store.ListResources(); err == nil {
		for _, r := range list {
			existing[r.ShareURL+"\x00"+r.FilePath] = true
		}
	}

	var (
		added   int
		skipped int
		errs    []string
	)

	// 广度优先遍历，带深度与数量上限。
	type node struct {
		path  string
		depth int
	}
	queue := []node{{path: req.Path, depth: 0}}
	for len(queue) > 0 && added < importMaxFiles {
		cur := queue[0]
		queue = queue[1:]

		entries, err := h.cache.ListShare(share, cur.path)
		if err != nil {
			errs = append(errs, cur.path+": "+err.Error())
			continue
		}
		for _, e := range entries {
			if e.IsDir {
				if cur.depth < importMaxDepth {
					queue = append(queue, node{path: e.Path, depth: cur.depth + 1})
				}
				continue
			}
			if !h.isVideo(e.Name) {
				continue
			}
			if existing[req.ShareURL+"\x00"+e.Path] {
				skipped++
				continue
			}
			if added >= importMaxFiles {
				break
			}
			title := strings.TrimSuffix(e.Name, path.Ext(e.Name))
			if _, err := h.store.AddResource(store.Resource{
				Title:    title,
				Provider: req.Provider,
				ShareURL: req.ShareURL,
				SharePwd: req.SharePwd,
				FilePath: e.Path,
			}); err != nil {
				errs = append(errs, e.Path+": "+err.Error())
				continue
			}
			existing[req.ShareURL+"\x00"+e.Path] = true
			added++
		}
	}

	resp := gin.H{"added": added, "skipped": skipped}
	if len(errs) > 0 {
		if len(errs) > 5 {
			errs = errs[:5]
		}
		resp["errors"] = errs
	}
	if added >= importMaxFiles {
		resp["note"] = "已达单次导入上限，可对子目录再次导入"
	}
	c.JSON(http.StatusOK, resp)
}
