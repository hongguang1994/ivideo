package resourcesearch

import (
	"strings"
	"testing"
)

func TestParseTelegramHTML(t *testing.T) {
	html := `<html><body>
<div class="tgme_widget_message_wrap">
  <div class="tgme_widget_message" data-post="movies/42">
    <div class="tgme_widget_message_text">流浪地球 4K<br/><a href="https://www.alipan.com/s/AbC123">网盘链接</a><br/>提取码: a1b2</div>
    <time datetime="2026-08-09T12:00:00+00:00"></time>
  </div>
</div></body></html>`
	items, err := parseTelegramHTML(strings.NewReader(html), "movies", "流浪地球")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %#v", items)
	}
	item := items[0]
	if item.Provider != "aliyun" || item.SharePwd != "a1b2" || item.Title != "流浪地球 4K" || item.SourceURL != "https://t.me/movies/42" {
		t.Fatalf("unexpected item: %#v", item)
	}
}

func TestParseTelegramHTMLFiltersUnrelatedMessages(t *testing.T) {
	html := `<div class="tgme_widget_message_wrap"><div class="tgme_widget_message" data-post="movies/1"><div class="tgme_widget_message_text">其他电影<a href="https://pan.quark.cn/s/abc">link</a></div></div></div>`
	items, err := parseTelegramHTML(strings.NewReader(html), "movies", "流浪地球")
	if err != nil || len(items) != 0 {
		t.Fatalf("unrelated message should be filtered: %#v, %v", items, err)
	}
}

func TestTelegramTitleReadsSeparatedNameField(t *testing.T) {
	if got := telegramTitle("名称：\n流浪地球2 (2023) 4K\n链接：", "fallback"); got != "流浪地球2 (2023) 4K" {
		t.Fatalf("unexpected title: %q", got)
	}
	if got := telegramTitle("名称\n：\n流浪地球\n链接", "fallback"); got != "流浪地球" {
		t.Fatalf("punctuation should be skipped: %q", got)
	}
}
