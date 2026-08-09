package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// SetupStatus 描述 Jellyfin 首次向导及 ivideo 专用 Key 的状态。
type SetupStatus struct {
	FirstUser string `json:"firstUser"`
	Ready     bool   `json:"ready"`
}

// SetupStatus 读取首次向导状态。不要读取 /Startup/User：该接口会在尚无用户时
// 隐式创建以系统用户命名的账户（容器中通常为 root）。
func (c *Client) SetupStatus(ctx context.Context) (SetupStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/Startup/Configuration", nil)
	if err != nil {
		return SetupStatus{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return SetupStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return SetupStatus{Ready: true}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return SetupStatus{}, readError("读取 Jellyfin 初始化状态", resp)
	}
	return SetupStatus{}, nil
}

// Bootstrap 创建第一个管理员、完成 Jellyfin 向导并生成 ivideo 专用 API Key。
// 仅允许在 Jellyfin 首次向导未完成时调用。
func (c *Client) Bootstrap(ctx context.Context, username, password string) (string, error) {
	status, err := c.SetupStatus(ctx)
	if err != nil {
		return "", err
	}
	if status.Ready {
		return "", fmt.Errorf("Jellyfin 已完成初始化，不能再创建首个管理员")
	}
	if err := c.ensureStartupUser(ctx); err != nil {
		return "", err
	}
	if err := c.postJSON(ctx, "/Startup/User", map[string]string{"Name": username, "Password": password}, ""); err != nil {
		return "", err
	}
	if err := c.postJSON(ctx, "/Startup/Complete", nil, ""); err != nil {
		return "", err
	}

	adminToken, err := c.authenticate(ctx, username, password)
	if err != nil {
		return "", err
	}
	if err := c.postJSON(ctx, "/Auth/Keys?"+url.Values{"app": {"ivideo"}}.Encode(), nil, adminToken); err != nil {
		return "", err
	}
	return c.findAPIKey(ctx, adminToken)
}

// ensureStartupUser 适配 Jellyfin 10.11+：更新首用户之前，服务端要求先生成它。
// 生成的系统默认名会被紧随其后的 /Startup/User 请求替换为 admin。
func (c *Client) ensureStartupUser(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/Startup/FirstUser", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return readError("创建 Jellyfin 首用户", resp)
	}
	return nil
}

func (c *Client) authenticate(ctx context.Context, username, password string) (string, error) {
	body, _ := json.Marshal(map[string]string{"Username": username, "Pw": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/Users/AuthenticateByName", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// Jellyfin binds the application identity from this header, not the JSON body.
	req.Header.Set("X-Emby-Authorization", `MediaBrowser Client="ivideo", Device="ivideo-server", DeviceId="ivideo-server", Version="1.0.0"`)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", readError("Jellyfin 管理员认证", resp)
	}
	var out struct {
		AccessToken string `json:"AccessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("Jellyfin 未返回管理员访问令牌")
	}
	return out.AccessToken, nil
}

func (c *Client) findAPIKey(ctx context.Context, adminToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/Auth/Keys", nil)
	if err != nil {
		return "", err
	}
	setTokenHeaders(req, adminToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", readError("读取 Jellyfin API Key", resp)
	}
	var out struct {
		Items []struct {
			AppName     string `json:"AppName"`
			AccessToken string `json:"AccessToken"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	for _, item := range out.Items {
		if item.AppName == "ivideo" && item.AccessToken != "" {
			return item.AccessToken, nil
		}
	}
	return "", fmt.Errorf("Jellyfin 未返回 ivideo API Key")
}

func (c *Client) postJSON(ctx context.Context, path string, value any, token string) error {
	var body io.Reader
	if value != nil {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if value != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		setTokenHeaders(req, token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return readError("调用 Jellyfin", resp)
	}
	return nil
}

func setTokenHeaders(req *http.Request, token string) {
	// 管理接口按 OpenAPI 要求读取标准 Authorization 头；X-Emby-Token 保留给
	// 仍使用旧头的 Jellyfin 版本。
	req.Header.Set("Authorization", `MediaBrowser Client="ivideo", Device="ivideo-server", DeviceId="ivideo-server", Version="1.0.0", Token="`+token+`"`)
	req.Header.Set("X-Emby-Token", token)
}

func readError(action string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("%s失败: %d: %s", action, resp.StatusCode, string(body))
}
