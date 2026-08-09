package githubcollector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"ivideo/server/internal/store"
)

func TestCollectorUsesTreeSHAAndSkipsUnchangedRawFiles(t *testing.T) {
	st, err := store.Open("sqlite", "file:github_collector_test?mode=memory&cache=shared")
	if err != nil { t.Fatal(err) }
	defer st.Close()
	var rawRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/commits/main"):
			_, _ = w.Write([]byte(`{"sha":"commit-1"}`))
		case strings.Contains(r.URL.Path, "/git/trees/commit-1"):
			_, _ = w.Write([]byte(`{"tree":[{"path":"resources.md","type":"blob","sha":"blob-1","size":120}]}`))
		case strings.Contains(r.URL.Path, "/commit-1/resources.md"):
			rawRequests.Add(1)
			_, _ = w.Write([]byte("| 资源名称 | 资源类型 | 分享链接 |\n| --- | --- | --- |\n| 测试动画 | 动漫 | https://pan.quark.cn/s/example |"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	collector := New(st, func() (string, error) { return "token", nil })
	collector.apiBase, collector.rawBase, collector.client = server.URL, server.URL, server.Client()
	if _, err := collector.CollectApproved(context.Background()); err != nil { t.Fatal(err) }
	if rawRequests.Load() != 1 { t.Fatalf("raw requests = %d, want 1", rawRequests.Load()) }
	items, total, err := st.ListGitHubObservedShares(1, 10)
	if err != nil { t.Fatal(err) }
	if total != 1 || len(items) != 1 || items[0].Title != "测试动画" { t.Fatalf("unexpected index: %#v total=%d", items, total) }
	if _, err := collector.CollectApproved(context.Background()); err != nil { t.Fatal(err) }
	if rawRequests.Load() != 1 { t.Fatalf("unchanged collection read raw file again: %d", rawRequests.Load()) }
}
