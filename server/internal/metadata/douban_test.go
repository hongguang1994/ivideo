package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDoubanSearchFallsBackToSuggestionWhenDetailsAreBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/suggest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"6510880","title":"超兽武装","year":"2011","url":"https://movie.douban.com/subject/6510880/","pic":"https://img.example/poster.jpg"}]`))
		case "/subject/6510880/":
			_, _ = w.Write([]byte(`<html><title>安全验证</title></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newDouban()
	client.http = server.Client()
	client.suggestURL = server.URL + "/suggest"
	client.subjectURL = server.URL + "/subject/"

	item, err := client.search(context.Background(), "超兽武装", 2011)
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != "6510880" || item.Title != "超兽武装" || item.Year != 2011 || item.Poster == "" {
		t.Fatalf("未使用搜索结果降级: %+v", item)
	}
}

func TestDoubanSearchRejectsConflictingYear(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"1","title":"同名作品","year":"2011"}]`))
	}))
	defer server.Close()

	client := newDouban()
	client.http = server.Client()
	client.suggestURL = server.URL
	if _, err := client.search(context.Background(), "同名作品", 2024); err == nil {
		t.Fatal("年份冲突时不应接受豆瓣候选")
	}
}

func TestDoubanDownloadUsesImageHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://movie.douban.com/" {
			http.Error(w, "missing referer", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-data"))
	}))
	defer server.Close()

	client := newDouban()
	client.http = server.Client()
	dst := filepath.Join(t.TempDir(), "poster.jpg")
	if err := client.download(context.Background(), server.URL, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "jpeg-data" {
		t.Fatalf("unexpected image data: %q", data)
	}
}
