package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/aliauth"
)

// Providers 返回各网盘的授权状态,供设置页展示。
// GET /api/settings/providers
func (h *Handler) Providers(c *gin.Context) {
	m, err := h.store.ListCredentialProviders()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 固定列出支持的网盘,标注是否已授权。
	out := []gin.H{
		{"provider": "aliyun", "name": "阿里云盘", "authMethod": "qrcode", "authorized": m["aliyun"]},
		{"provider": "aliyun_open", "name": "阿里云盘 · 开放接口(原画直链)", "authMethod": "token", "authorized": m["aliyun_open"]},
		{"provider": "115", "name": "115网盘", "authMethod": "cookie", "authorized": m["115"]},
		{"provider": "quark", "name": "夸克网盘", "authMethod": "cookie", "authorized": m["quark"]},
	}
	c.JSON(http.StatusOK, gin.H{"providers": out})
}

// SaveToken 保存某网盘的凭据(目前用于阿里开放接口 refresh token / 将来 115、夸克 cookie)。
// POST /api/settings/token  body: {provider, token}
func (h *Handler) SaveToken(c *gin.Context) {
	var req struct {
		Provider string `json:"provider"`
		Token    string `json:"token"`
		Extra    string `json:"extra"` // 阿里开放接口:alicloud_qr / alicloud_tv
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Provider == "" || req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 provider / token"})
		return
	}
	switch req.Provider {
	case "aliyun_open", "115", "quark":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的 provider: " + req.Provider})
		return
	}
	extra := strings.TrimSpace(req.Extra)
	if req.Provider == "aliyun_open" && extra == "" {
		extra = "alicloud_qr"
	}
	if err := h.store.SetCredential(req.Provider, strings.TrimSpace(req.Token), extra); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AliyunQR 申请阿里云盘登录二维码。
// POST /api/auth/aliyun/qr
func (h *Handler) AliyunQR(c *gin.Context) {
	sess, err := aliauth.Generate(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sess)
}

// AliyunQRStatus 轮询扫码状态;已确认则把 refresh_token 存库。
// POST /api/auth/aliyun/qr/status  body: {t, ck}
func (h *Handler) AliyunQRStatus(c *gin.Context) {
	var req struct {
		T  string `json:"t"`
		Ck string `json:"ck"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.T == "" || req.Ck == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 t / ck"})
		return
	}
	res, err := aliauth.Query(c.Request.Context(), req.T, req.Ck)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if res.Status == aliauth.StatusConfirmed && res.RefreshToken != "" {
		if err := h.store.SetCredentialToken("aliyun", res.RefreshToken); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存 token 失败: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": res.Status})
}
