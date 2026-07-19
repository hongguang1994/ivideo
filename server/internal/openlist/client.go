// Package openlist 是 OpenList(https://github.com/OpenListTeam/OpenList)
// REST API 的一个最小客户端，负责登录、列目录、取文件直链。
package openlist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Client 封装对 OpenList 的调用，并缓存登录 token。
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client

	mu    sync.Mutex
	token string
}

// New 创建一个 OpenList 客户端。
func New(baseURL, username, password string) *Client {
	return &Client{
		baseURL:  baseURL,
		username: username,
		password: password,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// FileItem 对应 OpenList 目录项的一部分字段。
type FileItem struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"is_dir"`
	Modified string `json:"modified"`
	Thumb    string `json:"thumb"`
	Type     int    `json:"type"`
}

// apiResp 是 OpenList 统一响应包裹。
type apiResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// login 获取并缓存 token。
func (c *Client) login() (string, error) {
	body, _ := json.Marshal(map[string]string{
		"username": c.username,
		"password": c.password,
	})
	var out struct {
		Token string `json:"token"`
	}
	if err := c.post("/api/auth/login", "", body, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// token 返回缓存的 token，必要时登录。
func (c *Client) getToken(force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && !force {
		return c.token, nil
	}
	tok, err := c.login()
	if err != nil {
		return "", err
	}
	c.token = tok
	return tok, nil
}

// List 列出指定目录下的条目。
func (c *Client) List(path string) ([]FileItem, error) {
	body, _ := json.Marshal(map[string]any{
		"path":     path,
		"password": "",
		"page":     1,
		"per_page": 0,
		"refresh":  false,
	})
	var out struct {
		Content []FileItem `json:"content"`
	}
	if err := c.authPost("/api/fs/list", body, &out); err != nil {
		return nil, err
	}
	return out.Content, nil
}

// RawURL 返回文件在网盘上的可直连播放地址。
func (c *Client) RawURL(path string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"path":     path,
		"password": "",
	})
	var out struct {
		RawURL string `json:"raw_url"`
	}
	if err := c.authPost("/api/fs/get", body, &out); err != nil {
		return "", err
	}
	return out.RawURL, nil
}

// authPost 带 token 调用，遇到鉴权失败会自动重登一次。
func (c *Client) authPost(path string, body []byte, out any) error {
	tok, err := c.getToken(false)
	if err != nil {
		return err
	}
	err = c.post(path, tok, body, out)
	if err != nil {
		// token 可能过期，强制重登再试一次。
		tok, lerr := c.getToken(true)
		if lerr != nil {
			return lerr
		}
		return c.post(path, tok, body, out)
	}
	return nil
}

// post 执行一次 POST，并解出 data 字段到 out。
func (c *Client) post(path, token string, body []byte, out any) error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("openlist: 解析响应失败: %w (body=%s)", err, string(raw))
	}
	if r.Code != 200 {
		return fmt.Errorf("openlist: 接口返回 code=%d message=%s", r.Code, r.Message)
	}
	if out != nil && len(r.Data) > 0 {
		return json.Unmarshal(r.Data, out)
	}
	return nil
}
