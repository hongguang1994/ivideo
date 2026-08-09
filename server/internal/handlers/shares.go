package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resp"
	"ivideo/server/internal/store"
)

// ListShares 列出收藏的所有分享。GET /api/shares
func (h *Handler) ListShares(c *gin.Context) {
	list, err := h.store.ListShares()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []store.Share{}
	}
	resp.OK(c, gin.H{"shares": list})
}

// AddShare 收藏一个分享。POST /api/shares
func (h *Handler) AddShare(c *gin.Context) {
	var sh store.Share
	if err := c.ShouldBindJSON(&sh); err != nil {
		resp.Fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	sh.Provider = strings.TrimSpace(sh.Provider)
	sh.ShareURL = strings.TrimSpace(sh.ShareURL)
	if sh.Provider == "" || sh.ShareURL == "" {
		resp.Fail(c, http.StatusBadRequest, "缺少 provider / shareUrl")
		return
	}
	if sh.ShareID == "" {
		sh.ShareID = extractShareID(sh.ShareURL)
	}
	if sh.Status == "" {
		sh.Status = "unknown"
	}
	id, err := h.store.AddShare(sh)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") || strings.Contains(strings.ToLower(msg), "duplicate") {
			resp.Fail(c, http.StatusConflict, "该分享已收藏过了")
			return
		}
		resp.Fail(c, http.StatusInternalServerError, msg)
		return
	}
	sh.ID = id
	resp.OK(c, sh)
}

type batchShareRequest struct {
	Items []store.Share `json:"items"`
}

type batchShareResult struct {
	Index    int    `json:"index"`
	ID       int64  `json:"id,omitempty"`
	Provider string `json:"provider"`
	ShareURL string `json:"shareUrl"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

// AddSharesBatch 批量收藏分享，并逐条返回成功、重复或失败状态。POST /api/shares/batch
func (h *Handler) AddSharesBatch(c *gin.Context) {
	var req batchShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(req.Items) == 0 {
		resp.Fail(c, http.StatusBadRequest, "没有可收藏的分享")
		return
	}
	if len(req.Items) > 500 {
		resp.Fail(c, http.StatusBadRequest, "一次最多收藏 500 条分享")
		return
	}

	existing, err := h.store.ListShares()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	seen := make(map[string]bool, len(existing)+len(req.Items))
	for _, sh := range existing {
		seen[batchShareKey(sh.Provider, sh.ShareURL)] = true
	}

	results := make([]batchShareResult, 0, len(req.Items))
	added, duplicates, failed := 0, 0, 0
	for i, sh := range req.Items {
		sh.Provider = strings.ToLower(strings.TrimSpace(sh.Provider))
		sh.ShareURL = strings.TrimSpace(sh.ShareURL)
		sh.SharePwd = strings.TrimSpace(sh.SharePwd)
		sh.Title = strings.TrimSpace(sh.Title)
		sh.Category = strings.TrimSpace(sh.Category)
		result := batchShareResult{Index: i, Provider: sh.Provider, ShareURL: sh.ShareURL}

		if sh.Provider == "" {
			sh.Provider = detectShareProvider(sh.ShareURL)
			result.Provider = sh.Provider
		}
		if message := validateBatchShare(sh); message != "" {
			result.Status = "failed"
			result.Message = message
			failed++
			results = append(results, result)
			continue
		}

		key := batchShareKey(sh.Provider, sh.ShareURL)
		duplicate := seen[key]
		if sh.ShareID == "" {
			sh.ShareID = extractShareID(sh.ShareURL)
		}
		if sh.Status == "" {
			sh.Status = "unknown"
		}
		id, addErr := h.store.AddShare(sh)
		if addErr != nil {
			result.Status = "failed"
			result.Message = addErr.Error()
			failed++
		} else {
			result.ID = id
			seen[key] = true
			if duplicate {
				result.Status = "duplicate"
				result.Message = "已收藏，已合并填写的信息"
				duplicates++
			} else {
				result.Status = "added"
				added++
			}
		}
		results = append(results, result)
	}

	resp.OK(c, gin.H{
		"added":      added,
		"duplicates": duplicates,
		"failed":     failed,
		"results":    results,
	})
}

func validateBatchShare(sh store.Share) string {
	if sh.ShareURL == "" {
		return "缺少分享链接"
	}
	if sh.Provider != "aliyun" && sh.Provider != "115" && sh.Provider != "quark" {
		return "仅支持阿里云盘、115 和夸克分享"
	}
	parsed, err := url.Parse(sh.ShareURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "分享链接格式不正确"
	}
	if detectShareProvider(sh.ShareURL) != sh.Provider {
		return "网盘类型与分享链接不匹配"
	}
	return ""
}

func detectShareProvider(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "alipan.com" || strings.HasSuffix(host, ".alipan.com"),
		host == "aliyundrive.com" || strings.HasSuffix(host, ".aliyundrive.com"):
		return "aliyun"
	case host == "115.com" || strings.HasSuffix(host, ".115.com"):
		return "115"
	case host == "quark.cn" || strings.HasSuffix(host, ".quark.cn"):
		return "quark"
	default:
		return ""
	}
}

func batchShareKey(provider, rawURL string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	rawURL = strings.TrimSpace(rawURL)
	if parsed, err := url.Parse(rawURL); err == nil {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.Fragment = ""
		rawURL = parsed.String()
	}
	return provider + "\x00" + rawURL
}

// UpdateShare 更新分享的可编辑字段。PUT /api/shares/:id
func (h *Handler) UpdateShare(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	var sh store.Share
	if err := c.ShouldBindJSON(&sh); err != nil {
		resp.Fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	sh.ID = id
	if err := h.store.UpdateShare(sh); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

// DeleteShare 删除一个收藏的分享。DELETE /api/shares/:id
func (h *Handler) DeleteShare(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	if err := h.store.DeleteShare(id); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

func parsePathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		resp.Fail(c, http.StatusBadRequest, "非法 id")
		return 0, false
	}
	return id, true
}

// extractShareID 从分享链接抽出分享码（aliyun/115/quark 都是 /s/<code> 形式）。
func extractShareID(url string) string {
	i := strings.Index(url, "/s/")
	if i < 0 {
		return ""
	}
	s := url[i+3:]
	for _, sep := range []string{"/", "?", "#"} {
		if j := strings.Index(s, sep); j >= 0 {
			s = s[:j]
		}
	}
	return strings.TrimSpace(s)
}
