package aliauth

// 阿里云盘「开放平台」OAuth 扫码授权，用于拿开放接口的 refresh_token（取原画直链要它）。
//
// 与本包里网页版(passport)扫码不同：这条走阿里官方开放平台端点，
// 需要自己在阿里开放平台注册应用拿到 client_id / client_secret。
// 端点与 scopes 参考社区通行实现(如 aliyundrive-webdav)：
//
//	POST {openBase}/oauth/authorize/qrcode      申请二维码 → sid + qrCodeUrl
//	GET  {openBase}/oauth/qrcode/{sid}/status   轮询扫码状态 → status + authCode
//	POST {openBase}/oauth/access_token          authCode 换 refresh_token

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// 开放平台申请的权限范围：读基本信息 + 读写文件。
var openScopes = []string{"user:base", "file:all:read", "file:all:write"}

// 扫码状态（阿里返回值）。
const (
	OpenStatusWaitLogin    = "WaitLogin"     // 待扫描
	OpenStatusScanSuccess  = "ScanSuccess"   // 已扫描，待确认
	OpenStatusLoginSuccess = "LoginSuccess"  // 已确认，可换 token
	OpenStatusExpired      = "QRCodeExpired" // 已过期
)

// OpenSession 是一次开放平台扫码会话。
type OpenSession struct {
	SID       string `json:"sid"`
	QRCodeURL string `json:"qrCodeUrl"`
}

// OpenGenerate 申请开放平台登录二维码。
func OpenGenerate(ctx context.Context, openBase, clientID, clientSecret string) (OpenSession, error) {
	body := map[string]any{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"scopes":        openScopes,
	}
	var out OpenSession
	if err := openPost(ctx, openBase+"/oauth/authorize/qrcode", body, &out); err != nil {
		return OpenSession{}, err
	}
	if out.SID == "" {
		return OpenSession{}, fmt.Errorf("未取到扫码会话 sid")
	}
	return out, nil
}

// OpenQueryStatus 轮询扫码状态；确认(LoginSuccess)时返回 authCode。
func OpenQueryStatus(ctx context.Context, openBase, sid string) (status, authCode string, err error) {
	var out struct {
		Status   string `json:"status"`
		AuthCode string `json:"authCode"`
	}
	url := fmt.Sprintf("%s/oauth/qrcode/%s/status", openBase, sid)
	if err := openGet(ctx, url, &out); err != nil {
		return "", "", err
	}
	return out.Status, out.AuthCode, nil
}

// OpenExchangeToken 用 authCode 换开放接口 refresh_token。
func OpenExchangeToken(ctx context.Context, openBase, clientID, clientSecret, authCode string) (string, error) {
	body := map[string]string{
		"grant_type":    "authorization_code",
		"code":          authCode,
		"client_id":     clientID,
		"client_secret": clientSecret,
	}
	var out struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := openPost(ctx, openBase+"/oauth/access_token", body, &out); err != nil {
		return "", err
	}
	if out.RefreshToken == "" {
		return "", fmt.Errorf("未取到 refresh_token")
	}
	return out.RefreshToken, nil
}

func openPost(ctx context.Context, url string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return openDo(req, out)
}

func openGet(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return openDo(req, out)
}

func openDo(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("阿里开放平台 %s 返回 %d: %s", req.URL.Path, resp.StatusCode, truncate(raw))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}
