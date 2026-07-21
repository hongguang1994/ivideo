package handlers

import (
	"net/http"
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
