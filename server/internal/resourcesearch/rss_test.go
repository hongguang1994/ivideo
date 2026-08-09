package resourcesearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRSSSourceCollectsDirectShare(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/feed.xml" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>师兄啊师兄</title><link>https://example.com/post</link><description><![CDATA[夸克：https://pan.quark.cn/s/abc123 提取码: a1b2]]></description><pubDate>2026-08-09</pubDate></item></channel></rss>`))
	}))
	defer server.Close()

	source := NewRSSSource(func(context.Context) ([]RSSFeed, error) {
		return []RSSFeed{{ID: "test", Name: "测试订阅", URL: server.URL + "/feed.xml", Enabled: true}}, nil
	})
	items, meta, err := source.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if meta.Scanned != 1 || len(items) != 1 {
		t.Fatalf("unexpected collection: meta=%#v items=%#v", meta, items)
	}
	item := items[0]
	if item.Provider != "quark" || item.SharePwd != "a1b2" || item.Title != "师兄啊师兄" || item.SourceName != "测试订阅" {
		t.Fatalf("unexpected item: %#v", item)
	}
}

func TestRSSSourceCanReadSameHostArticle(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/feed.xml":
			_, _ = writer.Write([]byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry><title>文章资源</title><link href="` + server.URL + `/article"/><updated>2026-08-09T10:00:00Z</updated></entry></feed>`))
		case "/article":
			_, _ = writer.Write([]byte(`<html><body><a href="https://www.alipan.com/s/AbC123">下载</a></body></html>`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	source := NewRSSSource(func(context.Context) ([]RSSFeed, error) {
		return []RSSFeed{{ID: "test", Name: "测试订阅", URL: server.URL + "/feed.xml", Enabled: true, FetchArticle: true}}, nil
	})
	items, _, err := source.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Provider != "aliyun" || items[0].SourceURL != server.URL+"/article" {
		t.Fatalf("unexpected items: %#v", items)
	}
}
