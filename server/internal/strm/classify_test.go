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
		{
			fp:    "/国漫/2012.熊出没/01.熊出没/熊出没.S01.E052.2012.mp4",
			title: "熊出没.S01.E052.2012",
			want:  MediaInfo{Kind: KindEpisode, Title: "熊出没", Categories: []string{"国漫", "2012.熊出没"}, Season: 1, Episode: 52},
		},
		{
			fp:    "/国漫/妖怪的名单/妖怪名单 第二季/妖怪名单 第二季 21.ts",
			title: "妖怪名单 第二季 21",
			want:  MediaInfo{Kind: KindEpisode, Title: "妖怪名单", Categories: []string{"国漫", "妖怪的名单"}, Season: 2, Episode: 21},
		},
		{
			fp:    "/国漫/尸兄/尸兄 第一季/尸兄-10.flv",
			title: "尸兄-10",
			want:  MediaInfo{Kind: KindEpisode, Title: "尸兄", Categories: []string{"国漫", "尸兄"}, Season: 1, Episode: 10},
		},
		{
			fp:    "/国漫/赛尔号/赛尔号 S12/赛尔号-03.mp4",
			title: "赛尔号-03",
			want:  MediaInfo{Kind: KindEpisode, Title: "赛尔号", Categories: []string{"国漫", "赛尔号"}, Season: 12, Episode: 3},
		},
		{
			fp:    "/国漫/2011-2019/2015.画江湖之灵主.41集全.720p/画江湖之灵主 - 第24集.MP4",
			title: "画江湖之灵主 - 第24集",
			want:  MediaInfo{Kind: KindEpisode, Title: "画江湖之灵主", Categories: []string{"国漫", "2011-2019"}, Season: 1, Episode: 24},
		},
		{
			fp:    "/国漫/2011-2019/2015.超级飞侠.1-12季/03/超级飞侠.S03E01香港怪物大作战.mp4",
			title: "超级飞侠.S03E01香港怪物大作战",
			want:  MediaInfo{Kind: KindEpisode, Title: "超级飞侠", Categories: []string{"国漫", "2011-2019", "2015.超级飞侠.1-12季"}, Season: 3, Episode: 1},
		},
		{
			fp:    "/国漫/2011-2019/2015.勇者大冒险.1-2季/勇者大冒险/1/05 - 5.flv",
			title: "05 - 5",
			want:  MediaInfo{Kind: KindEpisode, Title: "勇者大冒险", Categories: []string{"国漫", "2011-2019", "2015.勇者大冒险.1-2季", "勇者大冒险"}, Season: 1, Episode: 5},
		},
		{
			fp:    "/国漫/2011-2019/2015.勇者大冒险.1-2季/勇者大冒险/2/01 - 0.flv",
			title: "01 - 0",
			want:  MediaInfo{Kind: KindEpisode, Title: "勇者大冒险", Categories: []string{"国漫", "2011-2019", "2015.勇者大冒险.1-2季", "勇者大冒险"}, Season: 2, Episode: 1},
		},
		{
			fp:    "/国漫/2011-2019/2015.勇者大冒险.1-2季/勇者大冒险/电影版/1 - 上.flv",
			title: "1 - 上",
			want:  MediaInfo{Kind: KindEpisode, Title: "勇者大冒险", Categories: []string{"国漫", "2011-2019", "2015.勇者大冒险.1-2季", "勇者大冒险"}, Season: 0, Episode: 1},
		},
		{
			fp:    "/国漫/2011-2019/2015.勇者大冒险.1-2季/勇者大冒险/番外/1 - 1.flv",
			title: "1 - 1",
			want:  MediaInfo{Kind: KindEpisode, Title: "勇者大冒险", Categories: []string{"国漫", "2011-2019", "2015.勇者大冒险.1-2季", "勇者大冒险"}, Season: 0, Episode: 101},
		},
		{
			fp:    "/国漫/2011-2019/2015.爱神巧克力.1-2季/S02/Aishen Qiao Ke Li-SE02-EP15-1080P.mp4",
			title: "Aishen Qiao Ke Li-SE02-EP15-1080P",
			want:  MediaInfo{Kind: KindEpisode, Title: "爱神巧克力", Categories: []string{"国漫", "2011-2019"}, Season: 2, Episode: 15},
		},
		{
			fp:    "/国漫/2013.尸兄/尸兄 第一季/尸兄-39[高清版](1).flv",
			title: "尸兄-39[高清版](1)",
			want:  MediaInfo{Kind: KindEpisode, Title: "尸兄", Categories: []string{"国漫", "2013.尸兄"}, Season: 1, Episode: 39},
		},
		{
			fp:    "/国漫/2013.超神学院/超神学院 第1季/01 新三基友的暗袭.mp4",
			title: "01 新三基友的暗袭",
			want:  MediaInfo{Kind: KindEpisode, Title: "超神学院", Categories: []string{"国漫", "2013.超神学院"}, Season: 1, Episode: 1},
		},
		{
			fp:    "/国漫/2011-2019/2012.熊出没.1-12部+大电影.1080p/12.熊出没之怪兽计划/【百度云盘下载】04涂涂的徒弟_1080p.mp4",
			title: "【百度云盘下载】04涂涂的徒弟_1080p",
			want: MediaInfo{Kind: KindEpisode, Title: "熊出没之怪兽计划",
				Categories: []string{"国漫", "2011-2019", "2012.熊出没.1-12部+大电影.1080p"}, Season: 1, Episode: 4},
		},
		{
			fp:    "/国漫/2011-2019/2011.罗小黑战记.全集+电影/movie/【2019】4K.60帧.小黑.mkv",
			title: "【2019】4K.60帧.小黑",
			want:  MediaInfo{Kind: KindMovie, Title: "【2019】4K.60帧.小黑", Categories: []string{"国漫", "2011-2019", "2011.罗小黑战记.全集+电影", "movie"}, Year: 2019},
		},
		{
			fp:    "/国漫/2011-2019/2011.罗小黑战记.全集+电影/movie/《罗小黑战记》第8.5话 _超清 720P.mp4",
			title: "《罗小黑战记》第8.5话 _超清 720P",
			want:  MediaInfo{Kind: KindEpisode, Title: "罗小黑战记", Categories: []string{"国漫", "2011-2019", "2011.罗小黑战记.全集+电影"}, Season: 0, Episode: 85},
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
