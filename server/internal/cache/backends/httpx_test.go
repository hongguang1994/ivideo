package backends

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(fn roundTripFunc) *http.Client { return &http.Client{Transport: fn} }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestIsMarkupBody(t *testing.T) {
	cases := map[string]bool{
		`{"state":true}`:            false,
		`  {"a":1}`:                 false,
		`<!doctype html><html>`:     true,
		"\n  <?xml version=\"1.0\"": true, // 夸克直链失效返回的 XML
		``:                          false,
		`[1,2,3]`:                   false,
	}
	for body, want := range cases {
		if got := isMarkupBody([]byte(body)); got != want {
			t.Errorf("isMarkupBody(%q) = %v, want %v", body, got, want)
		}
	}
}

func TestDoHTTPSuccess(t *testing.T) {
	client := testClient(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("鉴权头未透传: %q", r.Header.Get("Authorization"))
		}
		return response(http.StatusOK, `{"state":true,"n":42}`), nil
	})

	var out struct {
		State bool `json:"state"`
		N     int  `json:"n"`
	}
	err := doHTTP(context.Background(), client, httpReq{
		Method:  http.MethodGet,
		URL:     "https://example.test/api",
		Headers: map[string]string{"Authorization": "Bearer tok"},
		Label:   "测试盘",
	}, &out)
	if err != nil {
		t.Fatalf("出错: %v", err)
	}
	if !out.State || out.N != 42 {
		t.Errorf("解析结果 = %+v", out)
	}
}

// 限流返回 HTML 时要给可读提示，而不是一堆 JSON 解析错误。
func TestDoHTTPDetectsMarkup(t *testing.T) {
	client := testClient(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "<!doctype html><title>405</title>"), nil
	})

	var out map[string]any
	err := doHTTP(context.Background(), client, httpReq{
		Method: http.MethodPost, URL: "https://example.test/api", Label: "115",
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "限流") {
		t.Errorf("应给出限流提示, 实际: %v", err)
	}
}

// 4xx（非限流）不重试，直接返回带响应体的错误。
func TestDoHTTPClientErrorNoRetry(t *testing.T) {
	hits := 0
	client := testClient(func(*http.Request) (*http.Response, error) {
		hits++
		return response(http.StatusBadRequest, `{"error":"bad param"}`), nil
	})

	err := doHTTP(context.Background(), client, httpReq{
		Method: http.MethodGet, URL: "https://example.test/api", Label: "夸克",
	}, nil)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if hits != 1 {
		t.Errorf("4xx 不该重试, 实际请求了 %d 次", hits)
	}
	if !strings.Contains(err.Error(), "bad param") {
		t.Errorf("错误里应带响应体: %v", err)
	}
}

// 429/5xx 要退避重试，恢复后应成功。
func TestDoHTTPRetriesOnRateLimit(t *testing.T) {
	hits := 0
	client := testClient(func(*http.Request) (*http.Response, error) {
		hits++
		if hits < 3 {
			return response(http.StatusTooManyRequests, ""), nil
		}
		return response(http.StatusOK, `{"ok":true}`), nil
	})

	var out struct {
		OK bool `json:"ok"`
	}
	err := doHTTP(context.Background(), client, httpReq{
		Method: http.MethodGet, URL: "https://example.test/api", Label: "阿里",
	}, &out)
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if !out.OK || hits != 3 {
		t.Errorf("out=%+v hits=%d, want ok/3", out, hits)
	}
}

func TestDoHTTPPostForm(t *testing.T) {
	client := testClient(func(r *http.Request) (*http.Response, error) {
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "pick_code=abc" {
			t.Errorf("表单未送达: %q", body)
		}
		return response(http.StatusOK, `{"state":true}`), nil
	})

	body, ctype := formBody(url.Values{"pick_code": {"abc"}})
	var out struct {
		State bool `json:"state"`
	}
	if err := doHTTP(context.Background(), client, httpReq{
		Method: http.MethodPost, URL: "https://example.test/api", Body: body, CType: ctype, Label: "115",
	}, &out); err != nil {
		t.Fatalf("出错: %v", err)
	}
	if !out.State {
		t.Error("应解析出 state=true")
	}
}

func TestDoHTTPDoesNotRetryWrite(t *testing.T) {
	hits := 0
	client := testClient(func(*http.Request) (*http.Response, error) {
		hits++
		return response(http.StatusTooManyRequests, "busy"), nil
	})
	err := doHTTP(context.Background(), client, httpReq{
		Method: http.MethodPost, URL: "https://example.test/transfer", Label: "115",
	}, nil)
	if err == nil || hits != 1 {
		t.Fatalf("POST 限流应只请求一次: err=%v hits=%d", err, hits)
	}
}

func TestDoHTTPDoesNotRetryTokenRenewal(t *testing.T) {
	hits := 0
	client := testClient(func(*http.Request) (*http.Response, error) {
		hits++
		return response(http.StatusTooManyRequests, "busy"), nil
	})
	err := doHTTP(context.Background(), client, httpReq{
		Method: http.MethodGet, URL: "https://example.test/token", Label: "115", NoRetry: true,
	}, nil)
	if err == nil || hits != 1 {
		t.Fatalf("令牌续期应只请求一次: err=%v hits=%d", err, hits)
	}
}

func TestDoHTTPRedactsQueryInError(t *testing.T) {
	client := testClient(func(*http.Request) (*http.Response, error) {
		return response(http.StatusBadRequest, "bad request"), nil
	})
	err := doHTTP(context.Background(), client, httpReq{
		Method: http.MethodGet, URL: "https://example.test/token?refresh_token=secret", Label: "测试盘",
	}, nil)
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "/token") {
		t.Fatalf("错误 URL 未脱敏: %v", err)
	}
}

func TestDoHTTPRetriesNetworkRead(t *testing.T) {
	hits := 0
	client := testClient(func(*http.Request) (*http.Response, error) {
		hits++
		if hits == 1 {
			return nil, errors.New("temporary network error")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(`{"ok":true}`)), Header: make(http.Header)}, nil
	})
	var out struct {
		OK bool `json:"ok"`
	}
	if err := doHTTP(context.Background(), client, httpReq{Method: http.MethodGet, URL: "https://example.test/api", Label: "测试盘"}, &out); err != nil || !out.OK || hits != 2 {
		t.Fatalf("网络重试失败: err=%v out=%+v hits=%d", err, out, hits)
	}
}
