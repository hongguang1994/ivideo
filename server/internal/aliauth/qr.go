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
	"log"
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

	// 诊断:打印结构(字段名+类型+长度,不打印敏感值),便于定位真正的 refresh_token。
	logBizExtShape(decoded)

	var payload struct {
		PdsLoginResult json.RawMessage `json:"pds_login_result"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return "", fmt.Errorf("bizExt JSON 解析失败: %w", err)
	}
	// pds_login_result 可能是对象,也可能是被再编码的 JSON 字符串。
	inner := payload.PdsLoginResult
	if len(inner) > 0 && inner[0] == '"' {
		var s string
		if err := json.Unmarshal(inner, &s); err == nil {
			inner = json.RawMessage(s)
		}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(inner, &fields); err != nil {
		return "", fmt.Errorf("pds_login_result 解析失败: %w", err)
	}
	// 优先 refreshToken;若缺失或过短(疑似取到短标识),再挑最像 token 的长字符串字段。
	rt := jsonString(fields["refreshToken"])
	if len(rt) < 40 {
		rt = pickTokenLike(fields, rt)
	}
	if rt == "" {
		return "", fmt.Errorf("bizExt 内未找到 refreshToken")
	}
	return rt, nil
}

// logBizExtShape 打印顶层 + pds_login_result 的字段名/类型/长度(不含值)。
func logBizExtShape(decoded []byte) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(decoded, &top); err != nil {
		log.Printf("[aliauth] bizExt 顶层非对象或解析失败,总长=%d", len(decoded))
		return
	}
	log.Printf("[aliauth] bizExt 顶层字段: %s", describeKeys(top))
	inner := top["pds_login_result"]
	if len(inner) > 0 && inner[0] == '"' {
		var s string
		if json.Unmarshal(inner, &s) == nil {
			inner = json.RawMessage(s)
		}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(inner, &fields) == nil {
		log.Printf("[aliauth] pds_login_result 字段: %s", describeKeys(fields))
	}
}

// describeKeys 返回 "key(type,len) key(type,len) ..." —— 仅字符串类型给出长度,不泄露值。
func describeKeys(m map[string]json.RawMessage) string {
	var b strings.Builder
	for k, v := range m {
		t := "?"
		n := 0
		if len(v) > 0 {
			switch v[0] {
			case '"':
				t = "str"
				var s string
				_ = json.Unmarshal(v, &s)
				n = len(s)
			case '{':
				t = "obj"
			case '[':
				t = "arr"
			case 't', 'f':
				t = "bool"
			default:
				t = "num"
			}
		}
		fmt.Fprintf(&b, "%s(%s,%d) ", k, t, n)
	}
	return b.String()
}

// pickTokenLike 在字段里挑一个最像 refresh token 的长字符串(排除已知的短标识)。
func pickTokenLike(fields map[string]json.RawMessage, cur string) string {
	best := cur
	for k, v := range fields {
		lk := strings.ToLower(k)
		if !strings.Contains(lk, "token") || strings.Contains(lk, "access") {
			continue // 只看 *token* 字段,排除 accessToken
		}
		s := jsonString(v)
		if len(s) > len(best) {
			best = s
		}
	}
	return best
}

func jsonString(v json.RawMessage) string {
	if len(v) == 0 || v[0] != '"' {
		return ""
	}
	var s string
	_ = json.Unmarshal(v, &s)
	return s
}

func truncate(b []byte) string {
	if len(b) > 300 {
		return string(b[:300]) + "…"
	}
	return string(b)
}
