package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ivideo/server/internal/strm"
)

func TestTMDbImagePathsAreDecoded(t *testing.T) {
	var item tmdbItem
	if err := json.Unmarshal([]byte(`{"poster_path":"/poster.jpg","backdrop_path":"/backdrop.jpg"}`), &item); err != nil {
		t.Fatal(err)
	}
	if item.PosterPath != "/poster.jpg" || item.BackdropPath != "/backdrop.jpg" {
		t.Fatalf("图片路径解析错误: %+v", item)
	}

	var episode tmdbEpisode
	if err := json.Unmarshal([]byte(`{"still_path":"/episode.jpg"}`), &episode); err != nil {
		t.Fatal(err)
	}
	if episode.StillPath != "/episode.jpg" {
		t.Fatalf("分集剧照路径解析错误: %+v", episode)
	}
}

func TestPreferredDiscoveryTermUsesCanonicalSeriesStem(t *testing.T) {
	items := []tmdbItem{{
		Title: "霍比特人3：五军之战", OriginalTitle: "The Hobbit: The Battle of the Five Armies",
	}}
	if got := preferredDiscoveryTerm("五军之战", items); got != "霍比特人3" {
		t.Fatalf("preferred term = %q, want 霍比特人3", got)
	}
}

func TestDiscoveryTitleStemKeepsPlainTitles(t *testing.T) {
	if got := discoveryTitleStem("指环王"); got != "指环王" {
		t.Fatalf("plain title changed to %q", got)
	}
}

func TestMatchConfidenceAllowsExactTitleWithoutYear(t *testing.T) {
	score, _ := matchConfidence("熊出没之夺宝熊兵", 0, tmdbItem{Title: "熊出没之夺宝熊兵", PosterPath: "/poster.jpg"})
	if score < 85 {
		t.Fatalf("score=%d, want automatic match", score)
	}
}

func TestMatchConfidenceRejectsConflictingYear(t *testing.T) {
	score, _ := matchConfidence("同名电影", 2024, tmdbItem{Title: "同名电影", ReleaseDate: "2014-01-01"})
	if score >= 85 {
		t.Fatalf("score=%d, want review", score)
	}
}

func TestRequiresAnimationHonorsStoredClassification(t *testing.T) {
	info := strm.MediaInfo{Categories: []string{"小雅动漫每日更新"}, OverrideLibrary: strm.LibTV}
	if requiresAnimation(info) {
		t.Fatal("已确认归入剧集的资源不应再次按动画搜索")
	}

	info.OverrideLibrary = ""
	if !requiresAnimation(info) {
		t.Fatal("未覆盖分类的动漫路径仍应按动画搜索")
	}
}

func TestRequiresAnimationForMovieInsideAnimePath(t *testing.T) {
	info := strm.MediaInfo{Kind: strm.KindMovie, Categories: []string{"国漫", "剧场版"}}
	if !requiresAnimation(info) {
		t.Fatal("动漫路径下的电影搜索必须要求动画类型")
	}
}

func TestMatchConfidenceRejectsAnimationConflict(t *testing.T) {
	score, reason := matchConfidenceWithEvidence("同名作品", 2024, tmdbItem{
		Title: "同名作品", ReleaseDate: "2024-01-01", GenreIDs: []int{16}, PosterPath: "/poster.jpg",
	}, 15, false)
	if score != 0 || reason != "动画属性冲突" {
		t.Fatalf("score=%d reason=%q", score, reason)
	}
}

func TestMatchConfidenceCombinesIndependentEvidence(t *testing.T) {
	score, _ := matchConfidenceWithEvidence("准确片名", 2024, tmdbItem{
		Title: "准确片名", ReleaseDate: "2024-03-01", PosterPath: "/poster.jpg",
	}, 15, false)
	if score < 90 {
		t.Fatalf("score=%d, exact title/year/type/path should auto publish", score)
	}
}

func TestInstallFallbackThumb(t *testing.T) {
	dir := t.TempDir()
	fanart := filepath.Join(dir, "fanart.jpg")
	thumb := filepath.Join(dir, "Season 01", "Demo S01E01-thumb.jpg")
	if err := os.WriteFile(fanart, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installFallbackThumb(fanart, thumb); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(thumb)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "image" {
		t.Fatalf("兜底剧照内容错误: %q", got)
	}
}

func TestWriteEpisodeNFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Season 01", "Demo S01E02.nfo")
	err := writeNFO(path, episodeNFO{
		Title:     "第二集 & 重逢",
		Plot:      "简介",
		Season:    1,
		Episode:   2,
		UniqueIDs: []uniqueID{{Type: "tmdb", Default: true, Value: "42"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	xml := string(b)
	for _, want := range []string{"<episodedetails>", "<season>1</season>", "<episode>2</episode>", "第二集 &amp; 重逢", `<uniqueid type="tmdb" default="true">42</uniqueid>`} {
		if !strings.Contains(xml, want) {
			t.Fatalf("NFO 缺少 %q:\n%s", want, xml)
		}
	}
}

func TestNormalizeTitle(t *testing.T) {
	if normalizeTitle("平清盛: 2012") != normalizeTitle("平 清盛：2012") {
		t.Fatal("标题归一化结果不同")
	}
}

func TestPickSearchResultRequiresExactAnimatedMatch(t *testing.T) {
	items := []tmdbItem{
		{ID: 1, Name: "仁者无敌", GenreIDs: []int{18}},
		{ID: 2, Name: "仁者无敌", GenreIDs: []int{16, 10759}},
		{ID: 3, Name: "相似但不同", GenreIDs: []int{16}},
	}
	got, ok := pickSearchResult(items, "仁者无敌", 0, true)
	if !ok || got.ID != 2 {
		t.Fatalf("应选择同名动画，got=%+v ok=%v", got, ok)
	}
	if _, ok := pickSearchResult(items, "不存在的动画", 0, true); ok {
		t.Fatal("不应回退到第一条搜索结果")
	}
}

func TestMetadataQueryTitleFromCollections(t *testing.T) {
	cases := map[string]struct{ title, filePath string }{
		"十万个冷笑话": {
			title:    "十万个冷笑话2",
			filePath: "/国漫/2011-2019/2012.十万个冷笑话.1-3季+剧场版.720p/十万个冷笑话2/02.flv",
		},
		"超兽武装": {
			title:    "仁者无敌",
			filePath: "/国漫/2011-2019/2011.超兽武装 勇者无惧+仁者无敌.66集全/仁者无敌/30.ts",
		},
		"勇者大冒险": {
			title:    "番外",
			filePath: "/国漫/2011-2019/2015.勇者大冒险.1-2季/勇者大冒险/番外/1.flv",
		},
	}
	for want, input := range cases {
		if got := metadataQueryTitle(input.title, input.filePath); got != want {
			t.Errorf("metadataQueryTitle(%q)=%q want %q", input.title, got, want)
		}
	}
}
