package mediaproxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenEnded(t *testing.T) {
	cases := map[string]bool{
		"":                  true,  // 没有 Range 头
		"bytes=0-":          true,  // ffprobe 探测就发这个
		"bytes=2471247042-": true,  // 播放器 seek
		"bytes=0-1048575":   false, // 有明确上界
		"bytes=100-200":     false,
	}
	for rng, want := range cases {
		if got := OpenEnded(rng); got != want {
			t.Errorf("OpenEnded(%q) = %v, want %v", rng, got, want)
		}
	}
}

// 上界越过文件末尾会让夸克忽略整个 Range 从头返回，必须钳制。
func TestBoundedEnd(t *testing.T) {
	const chunk = 32 << 20 // 32MB
	cases := []struct {
		name              string
		start, size, want int64
	}{
		{"文件中部：按块长", 0, 1 << 30, chunk - 1},
		{"接近末尾：钳到文件尾", 2471247042, 2473011088, 2473011087},
		{"正好末尾块", 2473011000, 2473011088, 2473011087},
		{"大小未知：不钳制", 1000, 0, 1000 + chunk - 1},
	}
	for _, c := range cases {
		if got := BoundedEnd(c.start, c.size, chunk); got != c.want {
			t.Errorf("%s: BoundedEnd(%d,%d)=%d, want %d", c.name, c.start, c.size, got, c.want)
		}
	}
}

func TestTotalFromContentRange(t *testing.T) {
	cases := map[string]int64{
		"bytes 0-33554431/1038876196": 1038876196,
		"bytes 100-200/300":           300,
		"":                            0,
		"garbage":                     0,
		"bytes 0-1/*":                 0, // 长度未知
	}
	for cr, want := range cases {
		if got := TotalFromContentRange(cr); got != want {
			t.Errorf("TotalFromContentRange(%q) = %d, want %d", cr, got, want)
		}
	}
}

// 上游桩：按 Range 返回对应片段，模拟网盘行为。
func fakeUpstream(t *testing.T, content []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rng := r.Header.Get("Range")
		total := int64(len(content))
		if rng == "" {
			w.Header().Set("Content-Length", fmt.Sprint(total))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}
		var start, end int64
		if _, err := fmt.Sscanf(rng, "bytes=%d-%d", &start, &end); err != nil {
			_, _ = fmt.Sscanf(rng, "bytes=%d-", &start)
			end = total - 1
		}
		if end > total-1 {
			end = total - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[start : end+1])
	}))
}

// 开放式区间必须拿到「从 N 到结尾」的完整内容 —— 只给一块会让转码读到意外 EOF。
func TestStreamOpenEndedGetsFullContent(t *testing.T) {
	content := []byte(strings.Repeat("A", 1000))
	up := fakeUpstream(t, content)
	defer up.Close()

	// 块长故意设得很小，强制走多段拼接
	p := New(128)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=0-")
	rec := httptest.NewRecorder()

	if err := p.Stream(rec, req, Target{URL: up.URL, Size: int64(len(content)), RangeMode: RangeChunked}); err != nil {
		t.Fatalf("Stream 出错: %v", err)
	}
	if rec.Code != http.StatusPartialContent {
		t.Errorf("状态码 = %d, want 206", rec.Code)
	}
	if got := rec.Body.Len(); got != len(content) {
		t.Errorf("拿到 %d 字节, want %d（分段拼接必须给足完整长度）", got, len(content))
	}
	if rec.Body.String() != string(content) {
		t.Error("拼接后的内容与源不一致")
	}
	if cr := rec.Header().Get("Content-Range"); cr != "bytes 0-999/1000" {
		t.Errorf("Content-Range = %q, want bytes 0-999/1000", cr)
	}
}

// 无 Range 头时对外应是完整响应 200，且不带 Content-Range。
func TestStreamNoRangeReturns200(t *testing.T) {
	content := []byte(strings.Repeat("B", 500))
	up := fakeUpstream(t, content)
	defer up.Close()

	p := New(128)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	if err := p.Stream(rec, req, Target{URL: up.URL, Size: int64(len(content)), RangeMode: RangeChunked}); err != nil {
		t.Fatalf("Stream 出错: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("状态码 = %d, want 200", rec.Code)
	}
	if cr := rec.Header().Get("Content-Range"); cr != "" {
		t.Errorf("无 Range 时不应带 Content-Range, 实际 %q", cr)
	}
	if rec.Body.Len() != len(content) {
		t.Errorf("拿到 %d 字节, want %d", rec.Body.Len(), len(content))
	}
}

// 有界 Range 原样透传。
func TestStreamBoundedRangePassthrough(t *testing.T) {
	content := []byte(strings.Repeat("C", 1000))
	up := fakeUpstream(t, content)
	defer up.Close()

	p := New(DefaultChunkBytes)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=10-19")
	rec := httptest.NewRecorder()

	if err := p.Stream(rec, req, Target{URL: up.URL, Size: int64(len(content))}); err != nil {
		t.Fatalf("Stream 出错: %v", err)
	}
	if rec.Code != http.StatusPartialContent || rec.Body.Len() != 10 {
		t.Errorf("状态=%d 长度=%d, want 206/10", rec.Code, rec.Body.Len())
	}
}

// 上游中途返回错误页（限流/直链过期）时必须停止拼接，
// 绝不能把错误页当视频数据写进流里。
func TestStreamStopsOnUpstreamError(t *testing.T) {
	const good = "GOODDATA"
	hits := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("Content-Range", "bytes 0-7/100")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte(good))
			return
		}
		// 第二段开始返回错误页（模拟夸克限流）
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html>403 Forbidden</html>"))
	}))
	defer up.Close()

	p := New(8)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=0-")
	rec := httptest.NewRecorder()

	if err := p.Stream(rec, req, Target{URL: up.URL, Size: 100, RangeMode: RangeChunked}); err != nil {
		t.Fatalf("Stream 出错: %v", err)
	}
	if body := rec.Body.String(); body != good {
		t.Errorf("流内容 = %q, want %q（错误页绝不能被拼进视频流）", body, good)
	}
}

// 首段就失败时应返回 error，让调用方渲染错误而不是写半截响应。
func TestStreamFirstChunkFailReturnsError(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer up.Close()

	p := New(128)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=0-")
	rec := httptest.NewRecorder()

	if err := p.Stream(rec, req, Target{URL: up.URL, Size: 100, RangeMode: RangeChunked}); err == nil {
		t.Error("首段失败时应返回 error")
	}
}

func TestStreamChunkedRejectsIgnoredRange(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // CDN 忽略 Range，错误地从文件头返回
		_, _ = w.Write([]byte("full file from start"))
	}))
	defer up.Close()

	p := New(128)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=10-")
	rec := httptest.NewRecorder()
	if err := p.Stream(rec, req, Target{URL: up.URL, Size: 100, RangeMode: RangeChunked}); err == nil {
		t.Fatal("上游忽略 Range 时应拒绝拼接")
	}
}

func TestParseContentRange(t *testing.T) {
	start, end, total, ok := ParseContentRange("bytes 10-19/100")
	if !ok || start != 10 || end != 19 || total != 100 {
		t.Fatalf("解析错误: start=%d end=%d total=%d ok=%v", start, end, total, ok)
	}
	if _, _, _, ok := ParseContentRange("bytes 10-9/100"); ok {
		t.Fatal("倒置范围不应通过")
	}
}

// 鉴权头必须原样带给上游（115 绑 UA、夸克绑 cookie）。
func TestStreamSendsAuthHeaders(t *testing.T) {
	var gotUA, gotCookie, gotReferer string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotCookie, gotReferer = r.Header.Get("User-Agent"), r.Header.Get("Cookie"), r.Header.Get("Referer")
		w.Header().Set("Content-Range", "bytes 0-3/4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("data"))
	}))
	defer up.Close()

	p := New(DefaultChunkBytes)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()

	tgt := Target{URL: up.URL, UA: "TestUA/1.0", Cookie: "k=v", Referer: "https://example.com/", Size: 4}
	if err := p.Stream(rec, req, tgt); err != nil {
		t.Fatalf("Stream 出错: %v", err)
	}
	if gotUA != tgt.UA || gotCookie != tgt.Cookie || gotReferer != tgt.Referer {
		t.Errorf("鉴权头未透传: UA=%q Cookie=%q Referer=%q", gotUA, gotCookie, gotReferer)
	}
}
