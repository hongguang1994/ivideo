// Package aliauth 实现阿里云盘网页版(passport)扫码登录：
// 申请二维码 → 用户手机 App 扫码确认 → 轮询拿到 web refresh_token。
//
// ⚠️ 端点与响应结构照社区通行实现整理，需真机扫码验证后再定型。
package aliauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	qrGenerateURL = "https://passport.aliyundrive.com/newlogin/qrcode/generate.do?appName=aliyun_drive&fromSite=52&appEntrance=web&isMobile=false&lang=zh_CN&returnUrl=&bizParams=&_bx-v=2.2.3"
	qrQueryURL    = "https://passport.aliyundrive.com/newlogin/qrcode/query.do?appName=aliyun_drive&fromSite=52&_bx-v=2.2.3"
)

// 二维码状态。
const (
	StatusNew       = "NEW"       // 已生成，待扫描
	StatusScanned   = "SCANED"    // 已扫描，待确认
	StatusConfirmed = "CONFIRMED" // 已确认，可取 token
	StatusExpired   = "EXPIRED"   // 已过期
	StatusCanceled  = "CANCELED"  // 已取消
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// Session 是一次扫码会话所需的标识 + 给前端渲染的二维码内容。
type Session struct {
	T         string `json:"t"`         // 会话标识
	Ck        string `json:"ck"`        // 会话标识
	QRContent string `json:"qrContent"` // 前端把它渲染成二维码图片
}

// QueryResult 是一次轮询的结果。
type QueryResult struct {
	Status       string `json:"status"`
	RefreshToken string `json:"-"` // 仅 CONFIRMED 时有
}

// Generate 申请一个登录二维码。
func Generate(ctx context.Context) (Session, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, qrGenerateURL, nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return Session{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var out struct {
		Content struct {
			Data struct {
				T           json.Number `json:"t"`
				CodeContent string      `json:"codeContent"`
				Ck          string      `json:"ck"`
			} `json:"data"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Session{}, fmt.Errorf("解析二维码响应失败: %w (body=%s)", err, truncate(raw))
	}
	d := out.Content.Data
	if d.CodeContent == "" || d.T == "" {
		return Session{}, fmt.Errorf("未取到二维码内容 (body=%s)", truncate(raw))
	}
	return Session{T: d.T.String(), Ck: d.Ck, QRContent: d.CodeContent}, nil
}

// Query 轮询扫码状态；CONFIRMED 时解出 refresh_token。
func Query(ctx context.Context, t, ck string) (QueryResult, error) {
	form := url.Values{"t": {t}, "ck": {ck}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, qrQueryURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return QueryResult{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var out struct {
		Content struct {
			Data struct {
				QrCodeStatus string `json:"qrCodeStatus"`
				BizExt       string `json:"bizExt"`
			} `json:"data"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return QueryResult{}, fmt.Errorf("解析轮询响应失败: %w (body=%s)", err, truncate(raw))
	}

	res := QueryResult{Status: out.Content.Data.QrCodeStatus}
	if res.Status == StatusConfirmed && out.Content.Data.BizExt != "" {
		rt, err := extractRefreshToken(out.Content.Data.BizExt)
		if err != nil {
			return res, err
		}
		res.RefreshToken = rt
	}
	return res, nil
}

// extractRefreshToken 从 bizExt(base64 编码的 JSON)里取 refreshToken。
func extractRefreshToken(bizExt string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(bizExt)
	if err != nil {
		// 有的返回是 URL-safe base64
		decoded, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(bizExt, "="))
		if err != nil {
			return "", fmt.Errorf("bizExt 解码失败: %w", err)
		}
	}
	var payload struct {
		PdsLoginResult struct {
			RefreshToken string `json:"refreshToken"`
		} `json:"pds_login_result"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return "", fmt.Errorf("bizExt JSON 解析失败: %w", err)
	}
	if payload.PdsLoginResult.RefreshToken == "" {
		return "", fmt.Errorf("bizExt 内未找到 refreshToken")
	}
	return payload.PdsLoginResult.RefreshToken, nil
}

func truncate(b []byte) string {
	if len(b) > 300 {
		return string(b[:300]) + "…"
	}
	return string(b)
}
