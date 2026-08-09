package resourcesearch

import (
	"context"
	"testing"
)

func TestCatalogSourceSearchesTitlesRemarksAndPaths(t *testing.T) {
	source := NewCatalogSource(func(context.Context) ([]CatalogEntry, error) {
		return []CatalogEntry{
			{Provider: "aliyun", ShareURL: "https://www.alipan.com/s/one", Title: "霍比特人3：五军之战", Status: "valid"},
			{Provider: "quark", ShareURL: "https://pan.quark.cn/s/two", Title: "合集", FilePath: "/电影/星际穿越/Interstellar.2014.mkv"},
			{Provider: "115", ShareURL: "https://115.com/s/three", Remark: "复仇者联盟4 终局之战", Status: "invalid"},
		}, nil
	})

	fiveArmies, _, err := source.Search(context.Background(), "五军之战")
	if err != nil || len(fiveArmies) != 1 || fiveArmies[0].Title != "霍比特人3：五军之战" {
		t.Fatalf("title search failed: items=%#v err=%v", fiveArmies, err)
	}
	interstellar, _, err := source.Search(context.Background(), "星际穿越")
	if err != nil || len(interstellar) != 1 || interstellar[0].FileName == "" {
		t.Fatalf("path search failed: items=%#v err=%v", interstellar, err)
	}
	invalid, _, err := source.Search(context.Background(), "终局之战")
	if err != nil || len(invalid) != 0 {
		t.Fatalf("invalid shares must be excluded: items=%#v err=%v", invalid, err)
	}
}

func TestCatalogSourceAllowsConservativeFuzzyMatches(t *testing.T) {
	source := NewCatalogSource(func(context.Context) ([]CatalogEntry, error) {
		return []CatalogEntry{
			{Provider: "aliyun", ShareURL: "https://www.alipan.com/s/one", Title: "霍比特人3：五军之战", Status: "valid"},
			{Provider: "quark", ShareURL: "https://pan.quark.cn/s/two", Title: "合集", FilePath: "/电影/星际穿越/Interstellar.2014.mkv"},
			{Provider: "115", ShareURL: "https://115.com/s/three", Title: "熊出没之探险日记2"},
		}, nil
	})

	typo, _, err := source.Search(context.Background(), "五军之站")
	if err != nil || len(typo) != 1 || typo[0].Title != "霍比特人3：五军之战" {
		t.Fatalf("one-rune typo should match: items=%#v err=%v", typo, err)
	}
	pathTypo, _, err := source.Search(context.Background(), "星际穿跃")
	if err != nil || len(pathTypo) != 1 || pathTypo[0].FileName == "" {
		t.Fatalf("path typo should match: items=%#v err=%v", pathTypo, err)
	}
	longTypo, _, err := source.Search(context.Background(), "熊出没之探险日纪2")
	if err != nil || len(longTypo) != 1 || longTypo[0].Title != "熊出没之探险日记2" {
		t.Fatalf("long title typo should match: items=%#v err=%v", longTypo, err)
	}
}

func TestCatalogSourceKeepsShortAndUnrelatedQueriesStrict(t *testing.T) {
	source := NewCatalogSource(func(context.Context) ([]CatalogEntry, error) {
		return []CatalogEntry{
			{Provider: "aliyun", ShareURL: "https://www.alipan.com/s/one", Title: "熊出没"},
			{Provider: "quark", ShareURL: "https://pan.quark.cn/s/two", Title: "星际穿越"},
		}, nil
	})

	shortTypo, _, err := source.Search(context.Background(), "熊出默")
	if err != nil || len(shortTypo) != 0 {
		t.Fatalf("short queries must not use fuzzy matching: items=%#v err=%v", shortTypo, err)
	}
	unrelated, _, err := source.Search(context.Background(), "流浪地球")
	if err != nil || len(unrelated) != 0 {
		t.Fatalf("unrelated query must not match: items=%#v err=%v", unrelated, err)
	}
}

func TestCatalogExactMatchesScoreAboveFuzzyMatches(t *testing.T) {
	exact, ok := catalogMatchScore("星际穿越", CatalogEntry{Title: "星际穿越"})
	if !ok {
		t.Fatal("exact match should be accepted")
	}
	fuzzy, ok := catalogMatchScore("星际穿跃", CatalogEntry{Title: "星际穿越"})
	if !ok {
		t.Fatal("fuzzy match should be accepted")
	}
	if exact <= fuzzy {
		t.Fatalf("exact score %d must exceed fuzzy score %d", exact, fuzzy)
	}
}
