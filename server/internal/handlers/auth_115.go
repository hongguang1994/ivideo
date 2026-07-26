package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/pan115auth"
	"ivideo/server/internal/resp"
)

// provider115Cookie 是 115 网页态 cookie 的凭据 key（与开放接口令牌 "115" 区分）。
// cookie 只用于转存（网页版 share/receive）；播放/列表仍用 "115" 的开放令牌。
const provider115Cookie = "115_cookie"

// Pan115QR 申请 115 网页扫码登录二维码。
// POST /api/auth/115/qr
func (h *Handler) Pan115QR(c *gin.Context) {
	sess, err := pan115auth.Token(c.Request.Context())
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	resp.OK(c, sess)
}

// Pan115QRStatus 轮询扫码状态；确认后换 cookie 并存库。
// POST /api/auth/115/qr/status  body: {uid,time,sign}
func (h *Handler) Pan115QRStatus(c *gin.Context) {
	var s pan115auth.Session
	if err := c.ShouldBindJSON(&s); err != nil || s.UID == "" {
		resp.Fail(c, http.StatusBadRequest, "缺少扫码会话")
		return
	}
	status, err := pan115auth.Status(c.Request.Context(), s)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	// 2 = 已确认，换取 cookie 并存库。
	if status == 2 {
		cookie, err := pan115auth.Exchange(c.Request.Context(), s)
		if err != nil {
			resp.Fail(c, http.StatusBadGateway, "换取 cookie 失败: "+err.Error())
			return
		}
		if err := h.store.SetCredential(provider115Cookie, cookie, "cookie"); err != nil {
			resp.Fail(c, http.StatusInternalServerError, "保存 cookie 失败: "+err.Error())
			return
		}
	}
	resp.OK(c, gin.H{"status": status})
}
