package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/quarkauth"
	"ivideo/server/internal/resp"
)

// providerQuarkCookie 是夸克网页态 cookie 的凭据 key。
// 夸克开放平台 API 需要用应用 secret 签名(x-pan-token)，中转令牌拿不到 secret，
// 所以夸克整条链路都走网页版 cookie。
const providerQuarkCookie = "quark_cookie"

// QuarkQR 申请夸克扫码登录二维码。
// POST /api/auth/quark/qr
func (h *Handler) QuarkQR(c *gin.Context) {
	sess, err := quarkauth.Token(c.Request.Context())
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	resp.OK(c, sess)
}

// QuarkQRStatus 轮询扫码状态；确认后换 cookie 并存库。
// POST /api/auth/quark/qr/status  body: {token}
func (h *Handler) QuarkQRStatus(c *gin.Context) {
	var s quarkauth.Session
	if err := c.ShouldBindJSON(&s); err != nil || s.Token == "" {
		resp.Fail(c, http.StatusBadRequest, "缺少扫码会话")
		return
	}
	status, ticket, err := quarkauth.Status(c.Request.Context(), s)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	if status == quarkauth.StatusConfirmed && ticket != "" {
		cookie, err := quarkauth.Exchange(c.Request.Context(), ticket)
		if err != nil {
			resp.Fail(c, http.StatusBadGateway, "换取 cookie 失败: "+err.Error())
			return
		}
		if err := h.store.SetCredential(providerQuarkCookie, cookie, "cookie"); err != nil {
			resp.Fail(c, http.StatusInternalServerError, "保存 cookie 失败: "+err.Error())
			return
		}
	}
	resp.OK(c, gin.H{"status": status})
}
