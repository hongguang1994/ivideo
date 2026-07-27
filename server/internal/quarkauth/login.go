// Package quarkauth 实现夸克网盘扫码登录，用于拿到「网页态 cookie」。
//
// 为什么用 cookie：夸克**开放平台**的 API（open-api-drive.quark.cn）要求公参
// x-pan-client-id / x-pan-tm / x-pan-token，其中 x-pan-token 是用应用 secret 算的签名；
// 而在线中转（oplist）只给 access_token、不给 secret，因此拿中转令牌**调不动**开放 API。
// 夸克网页版 API（drive-pc.quark.cn）用 cookie 鉴权，是免费号实际可用的那条路。
//
// 扫码流程（端点均已实测）：
//  1. Token()    取扫码 token + 二维码内容
//  2. Status()   轮询：50004001=未扫/等待，2000000=已确认(带 service ticket)
//  3. Exchange() 用 service ticket 换网页 cookie
package quarkauth

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
	uopBase  = "https://uop.quark.cn"
	panBase  = "https://pan.quark.cn"
	clientID = "532" // 夸克网页端 client_id（网页登录固定值）
	loginUA  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131 Safari/537.36"
)

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	// 不自动跟随重定向：换 cookie 时要读 302 响应里的 Set-Cookie。
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// Session 是一次扫码会话。
type Session struct {
	Token  string `json:"token"`  // 扫码 token，轮询/换 cookie 都要
	QRCode string `json:"qrcode"` // 二维码内容（前端渲染成图）
}

// 扫码状态码（夸克返回的 status）。
const (
	StatusWaiting   = 50004001 // 未扫码 / 结果为空
	StatusConfirmed = 2000000  // 已确认
)

// Token 申请一次扫码会话。
func Token(ctx context.Context) (Session, error) {
	u := fmt.Sprintf("%s/cas/ajax/getTokenForQrcodeLogin?client_id=%s&v=1.2&request_id=%d",
		uopBase, clientID, time.Now().UnixMilli())
	var out struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Data    struct {
			Members struct {
				Token string `json:"token"`
			} `json:"members"`
		} `json:"data"`
	}
	if err := getJSON(ctx, u, &out); err != nil {
		return Session{}, err
	}
	tok := out.Data.Members.Token
	if tok == "" {
		return Session{}, fmt.Errorf("夸克取扫码 token 失败: %s", out.Message)
	}
	qr := fmt.Sprintf("https://su.quark.cn/4_eMHBJ?token=%s&client_id=%s&ssb=weblogin&uc_param_str=&uc_biz_str=S%%3Acustom%%7CC%%3Atitlebar_fix",
		tok, clientID)
	return Session{Token: tok, QRCode: qr}, nil
}

// Status 轮询扫码状态，返回夸克原始 status 与 service ticket（已确认时才有）。
func Status(ctx context.Context, s Session) (status int, ticket string, err error) {
	u := fmt.Sprintf("%s/cas/ajax/getServiceTicketByQrcodeToken?client_id=%s&v=1.2&token=%s",
		uopBase, clientID, url.QueryEscape(s.Token))
	var out struct {
		Status int    `json:"status"`
		Data   struct {
			Members struct {
				ServiceTicket string `json:"service_ticket"`
			} `json:"members"`
		} `json:"data"`
	}
	if err := getJSON(ctx, u, &out); err != nil {
		return 0, "", err
	}
	return out.Status, out.Data.Members.ServiceTicket, nil
}

// Exchange 用 service ticket 换网页态 cookie 串。
func Exchange(ctx context.Context, ticket string) (string, error) {
	u := fmt.Sprintf("%s/account/info?st=%s&lw=scan", panBase, url.QueryEscape(ticket))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", loginUA)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	// 关键 cookie 从 Set-Cookie 里取（__uid/__puus 等）。
	var parts []string
	for _, c := range resp.Cookies() {
		if c.Value == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("夸克未返回 cookie（ticket 可能已失效）")
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
