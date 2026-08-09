package strm

import (
	"regexp"
	"strings"
)

// LibraryKind 是顶层大类，直接对应一个 Jellyfin 库。
type LibraryKind string

const (
	LibMovies  LibraryKind = "movies"  // 电影库（Movies 类型）
	LibTV      LibraryKind = "tv"      // 剧集库（TV Shows 类型）
	LibAnime   LibraryKind = "anime"   // 动漫库（TV Shows 类型，放番剧）
	LibVariety LibraryKind = "variety" // 综艺/纪录片库（TV Shows 类型）
	LibReview  LibraryKind = "review"  // 待整理（不参与自动刮削）
)

// animeMarkers 是判定「动漫」大类的关键词（出现在分类段里即算）。
var animeMarkers = []string{"动漫", "动画", "番剧", "国漫"}

var varietyMarkers = []string{"综艺", "真人秀", "脱口秀", "纪录片", "纪录", "晚会", "演唱会", "相声", "曲艺", "variety", "documentary", "talk show"}
var uncertainMovieTitle = regexp.MustCompile(`(?i)^(?:4k|8k|720p|1080p|2160p)(?:$|[ ._-]*(?:国语|粤语|普通话|上集|下集|hdr|hq|版|60帧)|[ ._-]*[（(])`)

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

// IsVariety 判断资源是否属于综艺/纪录片大类。只在分类段或标题中命中，避免误伤普通剧集。
func (m MediaInfo) IsVariety() bool {
	values := append([]string{}, m.Categories...)
	values = append(values, m.Title)
	for _, value := range values {
		lower := strings.ToLower(value)
		for _, marker := range varietyMarkers {
			if strings.Contains(lower, strings.ToLower(marker)) {
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
	// 审核/人工/后端确认的显式分类优先于路径推断。尤其是待整理资源会被
	// 临时按 movie 布局隔离，不能因此绕过 LibReview 回到电影库。
	if ValidLibrary(string(m.OverrideLibrary)) {
		return m.OverrideLibrary
	}
	if m.Kind == KindMovie {
		trimmed := strings.TrimSpace(m.Title)
		clean, _ := CleanMovieTitle(trimmed)
		// 技术占位名优先隔离，避免旧分类缓存把不同资源刮成同一部电影。
		if clean == "" || uncertainMovieTitle.MatchString(trimmed) {
			return LibReview
		}
		return LibMovies
	}
	if m.IsAnime() {
		return LibAnime
	}
	if m.Kind == KindEpisode && m.IsVariety() {
		return LibVariety
	}
	return LibTV
}

func ValidLibrary(value string) bool {
	switch LibraryKind(value) {
	case LibMovies, LibTV, LibAnime, LibVariety, LibReview:
		return true
	default:
		return false
	}
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
