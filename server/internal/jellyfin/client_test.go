package jellyfin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRefreshImagesByPath(t *testing.T) {
	var refreshed []string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Emby-Token") != "key" {
			t.Fatal("缺少 Jellyfin API Key")
		}
		if r.Method == http.MethodGet && r.URL.Path == "/Items" {
			body, _ := json.Marshal(itemsResp{Items: []Item{
				{ID: "series", Path: "/media/anime/勇者大冒险"},
				{ID: "episode", Path: "/media/anime/勇者大冒险/Season 01/E01.strm"},
				{ID: "other", Path: "/media/anime/其他作品/E01.strm"},
			}})
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/Refresh") {
			if r.URL.Query().Get("imageRefreshMode") != "FullRefresh" || r.URL.Query().Get("replaceAllImages") != "true" {
				t.Fatalf("刷新参数错误: %s", r.URL.RawQuery)
			}
			refreshed = append(refreshed, r.URL.Path)
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody}, nil
	})
	client := New("http://jellyfin", "key")
	client.http.Transport = transport

	count, err := client.RefreshImagesByPath("/media/anime/勇者大冒险")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || len(refreshed) != 2 {
		t.Fatalf("应只刷新目标作品的两个条目: count=%d paths=%v", count, refreshed)
	}
}

func TestRefreshImagesByPathsDeduplicatesItems(t *testing.T) {
	posts := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			body, _ := json.Marshal(itemsResp{Items: []Item{{ID: "episode", Path: "/media/anime/show/Season 01/E01.strm"}}})
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
		}
		if r.Method == http.MethodPost {
			posts++
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody}, nil
	})
	client := New("http://jellyfin", "key")
	client.http.Transport = transport
	count, err := client.RefreshImagesByPaths([]string{"/media/anime", "/media/anime/show"})
	if err != nil || count != 1 || posts != 1 {
		t.Fatalf("批量刷新应去重: count=%d posts=%d err=%v", count, posts, err)
	}
}

func TestRefreshCollectionImages(t *testing.T) {
	var deleted []string
	refreshed := false
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet && r.URL.Path == "/Library/VirtualFolders" {
			body, _ := json.Marshal([]virtualFolder{{ItemID: "anime-library", Name: "动漫"}})
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
		}
		if r.Method == http.MethodDelete {
			deleted = append(deleted, r.URL.Path)
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/Refresh") {
			refreshed = r.URL.Query().Get("imageRefreshMode") == "FullRefresh"
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody}, nil
	})
	client := New("http://jellyfin", "key")
	client.http.Transport = transport

	if err := client.RefreshCollectionImages("动漫"); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 || !refreshed {
		t.Fatalf("媒体库封面未完整刷新: deleted=%v refreshed=%v", deleted, refreshed)
	}
}

func TestEnforceLocalMetadataDisablesOnlineGuessing(t *testing.T) {
	var posted map[string]any
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet && r.URL.Path == "/Library/VirtualFolders" {
			body, _ := json.Marshal([]virtualFolder{{
				Name: "动漫", ItemID: "11111111-1111-1111-1111-111111111111",
				Locations:      []string{"/media/anime"},
				LibraryOptions: map[string]any{"Enabled": true, "PreferredMetadataLanguage": "zh-CN"},
			}})
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
		}
		if r.Method == http.MethodPost && r.URL.Path == "/Library/VirtualFolders/LibraryOptions" {
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatal(err)
			}
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody}, nil
	})
	client := New("http://jellyfin", "key")
	client.http.Transport = transport

	if err := client.enforceLocalMetadata("/media"); err != nil {
		t.Fatal(err)
	}
	options, ok := posted["LibraryOptions"].(map[string]any)
	if !ok || options["EnableInternetProviders"] != false {
		t.Fatalf("未关闭在线元数据: %#v", posted)
	}
	if options["Enabled"] != true || options["PreferredMetadataLanguage"] != "zh-CN" {
		t.Fatalf("原有媒体库设置未保留: %#v", options)
	}
	typeOptions, ok := options["TypeOptions"].([]any)
	if !ok || len(typeOptions) != 3 {
		t.Fatalf("剧集类型设置错误: %#v", options["TypeOptions"])
	}
	for _, raw := range typeOptions {
		option := raw.(map[string]any)
		if fetchers, ok := option["MetadataFetchers"].([]any); !ok || len(fetchers) != 0 {
			t.Fatalf("在线刮削器未清空: %#v", option)
		}
	}
}
