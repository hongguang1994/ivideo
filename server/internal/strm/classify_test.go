package strm

import "testing"

func TestParsePath(t *testing.T) {
	cases := []struct {
		fp, title string
		want      MediaInfo
	}{
		{
			fp:    "/小雅动漫每日更新/美国/星球大战：异等小队/S01/S01E01.mp4",
			title: "S01E01",
			want: MediaInfo{
				Kind: KindEpisode, Title: "星球大战：异等小队",
				Categories: []string{"小雅动漫每日更新", "美国"},
				Season:     1, Episode: 1,
			},
		},
		{
			fp:    "/小雅动漫每日更新/美国/星球大战：异等小队/S02/S02E09.mkv",
			title: "S02E09",
			want: MediaInfo{
				Kind: KindEpisode, Title: "星球大战：异等小队",
				Categories: []string{"小雅动漫每日更新", "美国"},
				Season:     2, Episode: 9,
			},
		},
		{
			fp:    "/优质韩国电影/孤胆特工.The.Man.From.Nowhere.2010.BD1080P.mkv",
			title: "孤胆特工.The.Man.From.Nowhere.2010.BD1080P",
			want: MediaInfo{
				Kind: KindMovie, Title: "孤胆特工.The.Man.From.Nowhere.2010.BD1080P",
				Categories: []string{"优质韩国电影"}, Year: 2010,
			},
		},
		{
			fp:    "/优质韩国电影/开心家族-国语1080P.mkv",
			title: "开心家族-国语1080P",
			want: MediaInfo{
				Kind: KindMovie, Title: "开心家族-国语1080P",
				Categories: []string{"优质韩国电影"}, Year: 0,
			},
		},
	}

	for _, c := range cases {
		got := ParsePath(c.fp, c.title)
		if got.Kind != c.want.Kind || got.Title != c.want.Title ||
			got.Year != c.want.Year || got.Season != c.want.Season || got.Episode != c.want.Episode {
			t.Errorf("ParsePath(%q):\n got  %+v\n want %+v", c.fp, got, c.want)
			continue
		}
		if len(got.Categories) != len(c.want.Categories) {
			t.Errorf("ParsePath(%q) categories = %v, want %v", c.fp, got.Categories, c.want.Categories)
			continue
		}
		for i := range got.Categories {
			if got.Categories[i] != c.want.Categories[i] {
				t.Errorf("ParsePath(%q) categories = %v, want %v", c.fp, got.Categories, c.want.Categories)
				break
			}
		}
	}
}
