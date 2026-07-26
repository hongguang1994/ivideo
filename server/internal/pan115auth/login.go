// Package pan115auth 实现 115 网页版扫码登录，用于拿到「网页态 cookie」。
//
// 为什么要 cookie：115 开放平台没有「转存他人分享」接口（实测 /open/share/* 全 404），
// 转存只能走网页版 API（webapi.115.com/share/receive），而它需要真实登录 cookie。
// 播放/列表/删除仍走更稳的开放平台 access_token —— cookie 只用于转存这一步。
//
// 扫码流程（全部实测验证过端点）：
//  1. Token()   取二维码内容 + 会话(uid/time/sign)
//  2. Status()  轮询扫码状态：0 等待 / 1 已扫 / 2 已确认 / <0 过期或取消
//  3. Exchange()确认后换取 cookie（UID/CID/SEID/KID）
package pan115auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	qrBase       = "https://qrcodeapi.115.com"
	passportBase = "https://passportapi.115.com"
	loginUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131 Safari/537.36"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Session 是一次扫码会话；Status/Exchange 都要带上它。
type Session struct {
	UID    string `json:"uid"`
	Time   int64  `json:"time"`
	Sign   string `json:"sign"`
	QRCode string `json:"qrcode"` // 二维码内容（前端渲染成二维码图）
}

// Token 申请一次扫码会话。
func Token(ctx context.Context) (Session, error) {
	var out struct {
		State int `json:"state"`
		Data  struct {
			UID    string `json:"uid"`
			Time   int64  `json:"time"`
			Sign   string `json:"sign"`
			QRCode string `json:"qrcode"`
		} `json:"data"`
	}
	if err := getJSON(ctx, qrBase+"/api/1.0/web/1.0/token/", &out); err != nil {
		return Session{}, err
	}
	if out.Data.UID == "" {
		return Session{}, fmt.Errorf("115 取二维码失败")
	}
	return Session{UID: out.Data.UID, Time: out.Data.Time, Sign: out.Data.Sign, QRCode: out.Data.QRCode}, nil
}

// Status 轮询扫码状态：0 等待 / 1 已扫 / 2 已确认 / 负数 过期或取消。
func Status(ctx context.Context, s Session) (int, error) {
	q := url.Values{
		"uid":  {s.UID},
		"time": {fmt.Sprintf("%d", s.Time)},
		"sign": {s.Sign},
		"_":    {fmt.Sprintf("%d", time.Now().UnixMilli())},
	}
	var out struct {
		State int `json:"state"`
		Data  struct {
			Status int `json:"status"`
		} `json:"data"`
	}
	// status 接口是长轮询，可能挂起到有变化或超时。
	if err := getJSON(ctx, qrBase+"/get/status/?"+q.Encode(), &out); err != nil {
		return 0, err
	}
	return out.Data.Status, nil
}

// Exchange 在扫码确认后换取网页态 cookie 串（形如 "UID=..; CID=..; SEID=..; KID=.."）。
func Exchange(ctx context.Context, s Session) (string, error) {
	body := url.Values{"account": {s.UID}, "app": {"web"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		passportBase+"/app/1.0/web/1.0/login/qrcode/", strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", loginUA)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var out struct {
		State int `json:"state"`
		Data  struct {
			Cookie map[string]string `json:"cookie"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("115 换 cookie 响应解析失败: %s", truncate(raw))
	}
	if len(out.Data.Cookie) == 0 {
		return "", fmt.Errorf("115 换 cookie 失败: %s", out.Message)
	}
	// 拼成标准 cookie 串，顺序固定便于阅读。
	var parts []string
	for _, k := range []string{"UID", "CID", "SEID", "KID"} {
		if v := out.Data.Cookie[k]; v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("115 cookie 字段为空")
	}
	return strings.Join(parts, "; "), nil
}

func getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", loginUA)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return json.Unmarshal(raw, out)
}

func truncate(b []byte) string {
	if len(b) > 160 {
		return string(b[:160])
	}
	return string(b)
}
