package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resp"

	"ivideo/server/internal/aliauth"
)

// Providers 返回各网盘的授权状态(含上次更新时间),供设置页展示。
// GET /api/settings/providers
func (h *Handler) Providers(c *gin.Context) {
	defs := []struct{ provider, name, method string }{
		{"aliyun", "阿里云盘", "qrcode"},
		{"aliyun_open", "阿里云盘 · 开放接口(原画直链)", "token"},
		{"115", "115网盘", "cookie"},
		{"quark", "夸克网盘", "cookie"},
	}
	out := make([]gin.H, 0, len(defs))
	for _, d := range defs {
		cr, found, _ := h.store.GetCredential(d.provider)
		out = append(out, gin.H{
			"provider":   d.provider,
			"name":       d.name,
			"authMethod": d.method,
			"authorized": found && cr.Token != "",
			"updatedAt":  cr.UpdatedAt, // 上次授权/更新时间(unix,0=从未)
		})
	}
	resp.OK(c, gin.H{"providers": out})
}

// CheckProvider 实测校验某网盘凭据是否仍有效(真去 ping 网盘换令牌)。
// POST /api/settings/providers/check  body: {provider}
// 注意:校验 aliyun 若触发刷新会轮换网页版 token(适配器会回写库,保持一致)。
func (h *Handler) CheckProvider(c *gin.Context) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Provider == "" {
		resp.Fail(c, http.StatusBadRequest, "缺少 provider")
		return
	}
	if err := h.cache.VerifyProvider(req.Provider); err != nil {
		// 校验失败不是接口错误,而是"令牌无效"的正常结果。
		resp.OK(c, gin.H{"healthy": false, "message": err.Error()})
		return
	}
	resp.OK(c, gin.H{"healthy": true, "message": "有效"})
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
		resp.Fail(c, http.StatusBadRequest, "缺少 provider / token")
		return
	}
	switch req.Provider {
	case "aliyun_open", "115", "quark":
	default:
		resp.Fail(c, http.StatusBadRequest, "不支持的 provider: "+req.Provider)
		return
	}
	extra := strings.TrimSpace(req.Extra)
	if req.Provider == "aliyun_open" && extra == "" {
		extra = "alicloud_qr"
	}
	if err := h.store.SetCredential(req.Provider, strings.TrimSpace(req.Token), extra); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

// AliyunOpenQR 申请「开放接口」扫码二维码（取原画直链要的令牌）。
// POST /api/auth/aliyun/open/qr
// 需先配置 aliyun.open_client_id / open_client_secret（自己在阿里开放平台注册应用）。
func (h *Handler) AliyunOpenQR(c *gin.Context) {
	if h.cfg.AliyunOpenClientID == "" || h.cfg.AliyunOpenClientSecret == "" {
		resp.Fail(c, http.StatusBadRequest,
			"未配置阿里开放平台 client_id/client_secret，无法自助扫码。请在配置里填入自己的应用凭据；或用 api.oplist.org 取到令牌后粘贴到下面。")
		return
	}
	sess, err := aliauth.OpenGenerate(c.Request.Context(), h.cfg.AliyunOpenBase,
		h.cfg.AliyunOpenClientID, h.cfg.AliyunOpenClientSecret)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	resp.OK(c, sess)
}

// AliyunOpenQRStatus 轮询开放接口扫码状态；确认后换 refresh_token 并存库。
// POST /api/auth/aliyun/open/qr/status  body: {sid}
func (h *Handler) AliyunOpenQRStatus(c *gin.Context) {
	var req struct {
		SID string `json:"sid"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.SID == "" {
		resp.Fail(c, http.StatusBadRequest, "缺少 sid")
		return
	}
	status, authCode, err := aliauth.OpenQueryStatus(c.Request.Context(), h.cfg.AliyunOpenBase, req.SID)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	if status == aliauth.OpenStatusLoginSuccess && authCode != "" {
		rt, err := aliauth.OpenExchangeToken(c.Request.Context(), h.cfg.AliyunOpenBase,
			h.cfg.AliyunOpenClientID, h.cfg.AliyunOpenClientSecret, authCode)
		if err != nil {
			resp.Fail(c, http.StatusBadGateway, "换取 refresh_token 失败: "+err.Error())
			return
		}
		if err := h.store.SetCredential("aliyun_open", rt, "alicloud_qr"); err != nil {
			resp.Fail(c, http.StatusInternalServerError, "保存令牌失败: "+err.Error())
			return
		}
	}
	resp.OK(c, gin.H{"status": status})
}

// AliyunQR 申请阿里云盘登录二维码。
// POST /api/auth/aliyun/qr
func (h *Handler) AliyunQR(c *gin.Context) {
	sess, err := aliauth.Generate(c.Request.Context())
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	resp.OK(c, sess)
}

// AliyunQRStatus 轮询扫码状态;已确认则把 refresh_token 存库。
// POST /api/auth/aliyun/qr/status  body: {t, ck}
func (h *Handler) AliyunQRStatus(c *gin.Context) {
	var req struct {
		T  string `json:"t"`
		Ck string `json:"ck"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.T == "" || req.Ck == "" {
		resp.Fail(c, http.StatusBadRequest, "缺少 t / ck")
		return
	}
	res, err := aliauth.Query(c.Request.Context(), req.T, req.Ck)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	if res.Status == aliauth.StatusConfirmed && res.RefreshToken != "" {
		if err := h.store.SetCredentialToken("aliyun", res.RefreshToken); err != nil {
			resp.Fail(c, http.StatusInternalServerError, "保存 token 失败: "+err.Error())
			return
		}
	}
	resp.OK(c, gin.H{"status": res.Status})
}
