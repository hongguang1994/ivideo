package strm

import "strings"

// LibraryKind 是顶层大类，直接对应一个 Jellyfin 库。
type LibraryKind string

const (
	LibMovies LibraryKind = "movies" // 电影库（Movies 类型）
	LibTV     LibraryKind = "tv"     // 剧集库（TV Shows 类型）
	LibAnime  LibraryKind = "anime"  // 动漫库（TV Shows 类型，放番剧）
)

// animeMarkers 是判定「动漫」大类的关键词（出现在分类段里即算）。
var animeMarkers = []string{"动漫", "动画", "番剧"}

// IsAnime 判断资源是否属于动漫大类（分类段里含动漫/动画/番剧字样）。
func (m MediaInfo) IsAnime() bool {
	for _, c := range m.Categories {
		for _, mk := range animeMarkers {
			if strings.Contains(c, mk) {
				return true
			}
		}
	}
	return false
}

// Library 返回资源应落入的顶层库。
//
//	电影 → movies
//	剧集且是动漫 → anime
//	剧集非动漫 → tv
//
// 动漫剧场版（电影结构 + 动漫标记）暂归 movies；等真有这类内容再单独处理。
func (m MediaInfo) Library() LibraryKind {
	if m.Kind == KindMovie {
		return LibMovies
	}
	if m.IsAnime() {
		return LibAnime
	}
	return LibTV
}

// countryAliases 把分类段里的地区写法归一到规范国家名。
// 以双字及以上关键词为主，避免单字（韩/日/美）误伤。顺序不敏感（包含即匹配）。
var countryAliases = []struct{ alias, canonical string }{
	{"中国大陆", "中国大陆"}, {"内地", "中国大陆"}, {"国产", "中国大陆"}, {"大陆", "中国大陆"},
	{"香港", "香港"}, {"港剧", "香港"}, {"港片", "香港"}, {"tvb", "香港"},
	{"台湾", "台湾"}, {"台剧", "台湾"},
	{"韩国", "韩国"}, {"韩剧", "韩国"}, {"韩漫", "韩国"},
	{"日本", "日本"}, {"日剧", "日本"}, {"日漫", "日本"}, {"日番", "日本"},
	{"美国", "美国"}, {"美剧", "美国"}, {"欧美", "欧美"},
	{"英国", "英国"}, {"英剧", "英国"},
	{"法国", "法国"}, {"德国", "德国"}, {"意大利", "意大利"}, {"西班牙", "西班牙"},
	{"泰国", "泰国"}, {"泰剧", "泰国"},
	{"印度", "印度"}, {"俄罗斯", "俄罗斯"}, {"苏联", "俄罗斯"},
	{"加拿大", "加拿大"}, {"澳大利亚", "澳大利亚"}, {"澳洲", "澳大利亚"},
	{"国漫", "中国大陆"},
}

// Country 从分类段推断国家/地区，取最靠近视频那层（最后一个匹配到的），无则空串。
// 例：["优质韩国电影"]→韩国；["小雅动漫每日更新","美国"]→美国。
func (m MediaInfo) Country() string {
	found := ""
	for _, c := range m.Categories {
		for _, a := range countryAliases {
			if strings.Contains(c, a.alias) {
				found = a.canonical // 后面的段覆盖前面的，取更具体的一层
			}
		}
	}
	return found
}
