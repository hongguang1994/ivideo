package resourcesearch

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeSourceResultCreatesStableAPIIdentity(t *testing.T) {
	item, ok := NormalizeSourceResult(SourceResult{
		Provider: "alipan", ShareURL: " HTTPS://PAN.QUARK.CN/s/Token?password=9x2a&amp;from=share ",
		Title: "师兄啊师兄", TitleBasis: TitleBasisMessage, SourceName: "@movies",
		Evidence: []string{"telegram:message-text"},
	}, "师兄啊师兄", SourceDescriptor{ID: "telegram-public", Name: "Telegram", Priority: 55})
	if !ok {
		t.Fatal("expected candidate to be normalized")
	}
	if item.Provider != "quark" || item.SharePwd != "9x2a" || item.Source != "telegram-public" {
		t.Fatalf("identity was not normalized: %#v", item)
	}
	if item.OriginalTitle != "师兄啊师兄" || item.TitleConfidence != 85 {
		t.Fatalf("title evidence was not normalized: %#v", item)
	}
	if len(item.MatchEvidence) != 4 {
		t.Fatalf("expected source evidence, got %#v", item.MatchEvidence)
	}
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"provider":"quark"`, `"source":"telegram-public"`, `"titleConfidence":85`} {
		if !strings.Contains(string(payload), field) {
			t.Fatalf("missing %s in %s", field, payload)
		}
	}
}

func TestNormalizeSourceResultMarksQueryFallbackAsLowConfidence(t *testing.T) {
	item, ok := NormalizeSourceResult(SourceResult{
		Provider: "115", ShareURL: "https://115.com/s/share-id",
	}, "流浪地球", SourceDescriptor{ID: "github-code", Name: "GitHub"})
	if !ok {
		t.Fatal("expected candidate to be normalized")
	}
	if item.Title != "流浪地球" || item.OriginalTitle != "" || item.TitleConfidence != 40 {
		t.Fatalf("query fallback must remain distinguishable: %#v", item)
	}
}
