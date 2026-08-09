package strm

import "testing"

func TestAnalyzeMediaPathUsesSpecificMovieFilename(t *testing.T) {
	analysis := AnalyzeMediaPath(
		"/国漫/2011-2019/2012.熊出没.1-12部+大电影.1080p/00.熊出没大电影/【百度云盘下载】熊出没之变形记1080p.mp4",
		"【百度云盘下载】熊出没之变形记1080p",
	)
	if analysis.Info.Kind != KindMovie || analysis.Info.Title != "熊出没之变形记" {
		t.Fatalf("unexpected media info: %+v", analysis.Info)
	}
	if analysis.SpecificTitle != "熊出没之变形记" {
		t.Fatalf("specific title = %q", analysis.SpecificTitle)
	}
	if analysis.CollectionTitle != "熊出没大电影" {
		t.Fatalf("collection title = %q", analysis.CollectionTitle)
	}
}

func TestAnalyzeMediaPathUsesMeaningfulParentForTechnicalFilename(t *testing.T) {
	analysis := AnalyzeMediaPath("/电影/Y英X雄S三Y元L里2026/4K/1080P.mkv", "1080P")
	if analysis.Info.Title != "英雄三元里" || analysis.Info.Year != 2026 {
		t.Fatalf("unexpected semantic title: %+v", analysis.Info)
	}
	if !hasSemanticToken(analysis.Tokens, "1080p", "technical") || !hasSemanticToken(analysis.Tokens, "2026", "year") {
		t.Fatalf("missing semantic tokens: %+v", analysis.Tokens)
	}
}

func TestAnalyzeMediaPathKeepsSeriesGrouping(t *testing.T) {
	analysis := AnalyzeMediaPath("/国漫/2011-2019/2015.勇者大冒险.1-2季/勇者大冒险/2/01.flv", "01")
	if analysis.Info.Kind != KindEpisode || analysis.Info.Title != "勇者大冒险" || analysis.Info.Episode != 1 {
		t.Fatalf("unexpected episode semantics: %+v", analysis.Info)
	}
	if analysis.SeriesTitle != "勇者大冒险" {
		t.Fatalf("series title = %q", analysis.SeriesTitle)
	}
}

func TestAnalyzeMediaPathKeepsLegitimateMixedTitle(t *testing.T) {
	analysis := AnalyzeMediaPath("/电影/X战警/X战警.2000.mkv", "X战警.2000")
	if analysis.Info.Title != "X战警" || analysis.Info.Year != 2000 {
		t.Fatalf("legitimate title was damaged: %+v", analysis.Info)
	}
}

func TestAnalyzeMediaPathPrefersCompleteParentWhenInitialsReplaceCharacters(t *testing.T) {
	analysis := AnalyzeMediaPath(
		"/Z拯救嫌疑人国版2023/拯J嫌Y人.2023.HD1080P.国语中字.mkv",
		"拯J嫌Y人.2023.HD1080P.国语中字",
	)
	if analysis.Info.Title != "拯救嫌疑人" || analysis.Info.Year != 2023 {
		t.Fatalf("incomplete obfuscated filename won over parent: %+v; candidates=%+v", analysis.Info, analysis.Candidates)
	}
}

func TestStripTitleQualifiers(t *testing.T) {
	for input, want := range map[string]string{
		"拯救嫌疑人国版":        "拯救嫌疑人",
		"周六血溅裳 印度 动作 惊悚": "周六血溅裳",
		"X战警":            "X战警",
	} {
		if got := stripTitleQualifiers(input); got != want {
			t.Fatalf("stripTitleQualifiers(%q) = %q, want %q", input, got, want)
		}
	}
}

func hasSemanticToken(tokens []PathToken, value, kind string) bool {
	for _, token := range tokens {
		if token.Value == value && token.Kind == kind {
			return true
		}
	}
	return false
}
