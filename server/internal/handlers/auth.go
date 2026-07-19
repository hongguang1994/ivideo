package handlers

import (
	"net/http"

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
		{"provider": "115", "name": "115网盘", "authMethod": "cookie", "authorized": m["115"]},
		{"provider": "quark", "name": "夸克网盘", "authMethod": "cookie", "authorized": m["quark"]},
	}
	c.JSON(http.StatusOK, gin.H{"providers": out})
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
