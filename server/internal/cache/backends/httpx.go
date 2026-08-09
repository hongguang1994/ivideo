package backends

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 各网盘适配器共用的 HTTP 调用层。
//
// 抽出来不只是去重：三家原本各写一套，只有阿里做了状态码检查和限流重试，
// 115/夸克都没有 —— 而这两家限流时会返回 **HTML 错误页**，
// 不检查状态码就会当成 JSON 去解析，报出一堆看不懂的解析错误
// （实测 115 限流返回 405 页面、夸克直链失效返回 XML）。
// 统一到这里后三家都自动获得：状态码检查 + HTML 识别 + 限流退避重试。

// httpReq 描述一次网盘接口调用。
type httpReq struct {
	Method     string               // GET / POST
	URL        string               // 完整地址（含 query）
	Headers    map[string]string    // 鉴权等请求头
	Body       []byte               // 已编码的请求体，nil 表示无
	CType      string               // Content-Type，Body 非空时必填
	Retry      bool                 // 显式允许重试（POST 等有副作用的请求默认不重试）
	NoRetry    bool                 // 强制只请求一次（令牌刷新等 GET 也不应重试）
	Label      string               // 错误信息里的网盘名，如 "阿里" / "115" / "夸克"
	OnResponse func(*http.Response) // 读取响应体前的钩子（例如接收刷新后的 cookie）
}

// jsonBody 把值编码成 JSON 请求体。
func jsonBody(v any) ([]byte, string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, "", err
	}
	return b, "application/json", nil
}

// formBody 把表单编码成请求体。
func formBody(v url.Values) ([]byte, string) {
	return []byte(v.Encode()), "application/x-www-form-urlencoded"
}

// doHTTP 发起调用并把响应 JSON 解到 out（out 为 nil 时只校验状态码）。
// GET/HEAD 与显式标记 Retry 的请求会对 429/408/5xx 指数退避重试；
// 转存、删除等写操作默认只发一次，避免网络抖动时重复执行。
func doHTTP(ctx context.Context, client *http.Client, r httpReq, out any) error {
	maxAttempts := 1
	if !r.NoRetry && (r.Retry || r.Method == http.MethodGet || r.Method == http.MethodHead) {
		maxAttempts = 4
	}
	delay := 500 * time.Millisecond
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay *= 2
		}

		var rd io.Reader
		if r.Body != nil {
			rd = bytes.NewReader(r.Body)
		}
		req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, rd)
		if err != nil {
			return err
		}
		if r.CType != "" {
			req.Header.Set("Content-Type", r.CType)
		}
		for k, v := range r.Headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err // 网络抖动，重试
			continue
		}
		if r.OnResponse != nil {
			r.OnResponse(resp)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("%s接口读取响应失败: %w", r.Label, err)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("%s接口 %s 返回 %d: %s",
				r.Label, safeURL(r.URL), resp.StatusCode, truncateBody(raw))
			if !retryableStatus(resp.StatusCode) {
				return lastErr
			}
			continue
		}

		if out == nil || len(raw) == 0 {
			return nil
		}

		// 限流时网盘常返回 HTML/XML 错误页而非 JSON（状态码却是 200）。
		// 先识别出来给可读提示，否则只会看到一串 JSON 解析错误。
		if isMarkupBody(raw) {
			lastErr = fmt.Errorf("%s接口暂时限流（返回了网页而非数据），请稍后再试", r.Label)
			continue
		}

		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s接口响应解析失败: %w (%s)", r.Label, err, truncateBody(raw))
		}
		return nil
	}
	return lastErr
}

// safeURL 保留接口定位信息，但不把令牌、cookie 等 query 参数写进错误日志。
func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid URL>"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "?")
}

// isMarkupBody 判断响应体是否是 HTML/XML（网盘限流或出错时的错误页）。
func isMarkupBody(raw []byte) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue // 跳过前导空白
		case '<':
			return true
		default:
			return false
		}
	}
	return false
}
