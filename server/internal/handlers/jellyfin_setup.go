package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/jellyfin"
	"ivideo/server/internal/resp"
)

const jellyfinCredentialProvider = "jellyfin_api"

// JellyfinStatus 返回 Jellyfin 首次安装与 ivideo 连接状态，不暴露 API Key。
func (h *Handler) JellyfinStatus(c *gin.Context) {
	if h.cfg.JellyfinBaseURL == "" {
		resp.Fail(c, http.StatusBadRequest, "未配置 jellyfin.base_url")
		return
	}
	client := jellyfin.New(h.cfg.JellyfinBaseURL, "")
	status, err := client.SetupStatus(c.Request.Context())
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	credential, found, _ := h.store.GetCredential(jellyfinCredentialProvider)
	connected := found && credential.Token != ""
	resp.OK(c, gin.H{
		"ready":         status.Ready,
		"firstUser":     status.FirstUser,
		"connected":     connected,
		"baseUrl":       h.cfg.JellyfinBaseURL,
		"canInitialize": !status.Ready && !connected,
	})
}

func (h *Handler) RefreshJellyfinImages(c *gin.Context) {
	if h.jf == nil {
		resp.Fail(c, http.StatusBadRequest, "Jellyfin 尚未连接")
		return
	}
	var req struct {
		Path    string `json:"path"`
		Library string `json:"library"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, http.StatusBadRequest, "请求内容不正确")
		return
	}
	if strings.TrimSpace(req.Library) != "" {
		if err := h.jf.RefreshCollectionImages(strings.TrimSpace(req.Library)); err != nil {
			resp.Fail(c, http.StatusBadGateway, err.Error())
			return
		}
		resp.OK(c, gin.H{"library": strings.TrimSpace(req.Library), "refreshed": true})
		return
	}
	mediaDir := filepath.Clean(h.cfg.MediaDir)
	target := filepath.Clean(req.Path)
	if target == mediaDir || !strings.HasPrefix(target, mediaDir+string(filepath.Separator)) {
		resp.Fail(c, http.StatusBadRequest, "只能刷新媒体目录中的具体作品")
		return
	}
	count, err := h.jf.RefreshImagesByPath(target)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	resp.OK(c, gin.H{"refreshed": count})
}

// InitializeJellyfin 创建首个管理员和 ivideo 专用 API Key。密码只通过本次响应返回。
func (h *Handler) InitializeJellyfin(c *gin.Context) {
	if h.cfg.JellyfinBaseURL == "" {
		resp.Fail(c, http.StatusBadRequest, "未配置 jellyfin.base_url")
		return
	}
	if credential, found, _ := h.store.GetCredential(jellyfinCredentialProvider); found && credential.Token != "" {
		resp.Fail(c, http.StatusConflict, "Jellyfin 已初始化")
		return
	}
	password, err := randomPassword()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	client := jellyfin.New(h.cfg.JellyfinBaseURL, "")
	key, err := client.Bootstrap(c.Request.Context(), "admin", password)
	if err != nil {
		resp.Fail(c, http.StatusBadGateway, err.Error())
		return
	}
	if err := h.store.SetCredential(jellyfinCredentialProvider, key, "jellyfin_api_key"); err != nil {
		resp.Fail(c, http.StatusInternalServerError, "保存 Jellyfin API Key 失败: "+err.Error())
		return
	}
	h.jf = jellyfin.New(h.cfg.JellyfinBaseURL, key)
	if err := h.jf.EnsureLibraries(h.cfg.MediaDir); err != nil {
		resp.Fail(c, http.StatusBadGateway, "创建 Jellyfin 媒体库失败: "+err.Error())
		return
	}
	if err := h.jf.RefreshLibrary(); err != nil {
		resp.Fail(c, http.StatusBadGateway, "扫描 Jellyfin 媒体库失败: "+err.Error())
		return
	}
	h.cache.SetSessionSource(cache.NewJellyfinSessions(h.jf, h.cfg.MediaDir))
	resp.OK(c, gin.H{"username": "admin", "password": password, "baseUrl": h.cfg.JellyfinBaseURL})
}

func randomPassword() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
