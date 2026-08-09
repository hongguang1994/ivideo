package strm

import "testing"

func TestLibraryAndCountry(t *testing.T) {
	cases := []struct {
		fp, title   string
		wantLib     LibraryKind
		wantCountry string
	}{
		{"/小雅动漫每日更新/美国/星球大战：异等小队/S01/S01E01.mp4", "S01E01", LibAnime, "美国"},
		{"/优质韩国电影/82年生的金智英.Kim.Ji-young.Born.1982.HD1080P.韩语中字.mp4", "82年生的金智英", LibMovies, "韩国"},
		{"/优质韩国电影/孤胆特工.The.Man.From.Nowhere.2010.mkv", "孤胆特工", LibMovies, "韩国"},
		// 假想的扩展样本（验证词典与大类判定）
		{"/日本动漫/进击的巨人/S01/S01E01.mkv", "S01E01", LibAnime, "日本"},
		{"/国产剧/港剧/龙兄虎弟/S01/S01E01.mkv", "S01E01", LibTV, "香港"},
		{"日剧/平清盛/平清盛.S01E12.mkv", "平清盛.S01E12", LibTV, "日本"},
		{"/综艺/韩国/Running Man/S01/S01E01.mp4", "S01E01", LibVariety, "韩国"},
		{"/纪录片/美国/Planet Earth/S01/S01E01.mkv", "S01E01", LibVariety, "美国"},
		{"/欧美电影/片名.2020.mkv", "片名", LibMovies, "欧美"},
		{"/未知/4K国语上集(1).mkv", "4K国语上集(1)", LibReview, ""},
	}
	for _, c := range cases {
		info := ParsePath(c.fp, c.title)
		if got := info.Library(); got != c.wantLib {
			t.Errorf("%s: Library=%s want %s", c.fp, got, c.wantLib)
		}
		if got := info.Country(); got != c.wantCountry {
			t.Errorf("%s: Country=%q want %q", c.fp, got, c.wantCountry)
		}
	}
}

func TestTechnicalMovieTitleWithSpaceNeedsReview(t *testing.T) {
	info := MediaInfo{Kind: KindMovie, Title: "4K HDR"}
	if got := info.Library(); got != LibReview {
		t.Fatalf("Library=%s want %s", got, LibReview)
	}
}
