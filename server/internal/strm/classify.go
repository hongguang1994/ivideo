package strm

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// MediaKind 是媒体大类，决定 strm 落进哪个库、用什么目录结构。
type MediaKind string

const (
	KindMovie   MediaKind = "movie"   // 电影：<片名> (年份)/<片名>.strm
	KindEpisode MediaKind = "episode" // 剧集分集：<剧名>/Season xx/<剧名> SxxExx.strm
)

// MediaInfo 是从 file_path 解析出的分类信息。
type MediaInfo struct {
	Kind            MediaKind
	Categories      []string // 分类段（题材/国别层），如 ["优质韩国电影"] 或 ["小雅动漫每日更新","美国"]
	Title           string   // 电影=片名；剧集=剧名
	Year            int      // 电影年份，0 表示未知
	Season          int      // 剧集季号
	Episode         int      // 剧集集号
	OverrideLibrary LibraryKind
}

var (
	// 匹配 .../Sxx/SxxEyy.<ext>：季目录 + 分集文件。大小写不敏感。
	// 季目录下的分集文件：SxxExx 可以**嵌在**文件名任意位置，
	// 如 /S02/癫狂记.Big.Train.S02E06.2002.mkv，不要求整个文件名就是集号。
	reEpisodeFile = regexp.MustCompile(`(?i)/S(\d{1,3})/[^/]*S(?:E)?(\d{1,3})[ ._-]*E(?:P)?(\d{1,3})[^/]*\.[a-z0-9]+$`)
	// 没有独立季目录，但文件名带 SxxEyy，例如：
	// /日剧/平清盛/平清盛.S01E12.mkv。
	reEpisodeName = regexp.MustCompile(`(?i)(?:^|[ ._-])S(?:E)?(\d{1,3})[ ._-]*E(?:P)?(\d{1,3})(?:[^0-9]|$)`)
	// 从标题里提取 4 位年份（1900–2099）。
	reYear = regexp.MustCompile(`(19|20)\d{2}`)
	// 季目录名 Sxx。
	reSeasonDir = regexp.MustCompile(`(?i)^S(\d{1,3})$`)
	// 父目录中的季号，如「妖怪名单 第一季」「赛尔号 S02」。
	reNamedSeason = regexp.MustCompile(`(?i)(?:第([0-9一二三四五六七八九十]+)季|(?:^|[ ._-])S(\d{1,3})(?:$|[ ._-]))`)
	// 带标题的尾号分集，如「尸兄-10」「妖怪名单 第一季 01」。
	reTrailingEpisode    = regexp.MustCompile(`(?i)(?:^|[ ._-])(?:EP?|第)?(\d{1,3})(?:集|话|話)?$`)
	reChineseEpisode     = regexp.MustCompile(`第\s*(\d{1,3})\s*(?:集|话|話)`)
	reFractionalEpisode  = regexp.MustCompile(`第\s*(\d{1,3})[.]([0-9])\s*(?:集|话|話)`)
	reBareChineseEpisode = regexp.MustCompile(`第\s*(\d{1,3})\s*$`)
	reLeadingEpisode     = regexp.MustCompile(`^(\d{1,3})(?:[ ._-]+|[^0-9])`)
	reDownloadEpisode    = regexp.MustCompile(`【(?:百度云盘|百度网盘)下载】\s*(\d{1,3})(?:[^0-9]|$)`)
	reTaggedEpisode      = regexp.MustCompile(`(?i)(?:^|[ ._-])(\d{1,3})\s*[\[(（].*$`)
	reLeadingIndex       = regexp.MustCompile(`^\d{1,3}[._ -]+`)
	reLeadingYear        = regexp.MustCompile(`^(?:19|20)\d{2}[._ -]+`)
)

// ParsePath 从 file_path（分享内的绝对路径）解析媒体分类信息。
// title 兜底用于电影缺路径信息时。
func ParsePath(filePath, title string) MediaInfo {
	segs := splitClean(filePath)

	// —— 剧集：路径里出现 /Sxx/SxxEyy.ext —— //
	if m := reEpisodeFile.FindStringSubmatch(filePath); m != nil {
		season, _ := strconv.Atoi(m[2])
		episode, _ := strconv.Atoi(m[3])
		// 找到季目录 Sxx 的下标：剧名是它的父段，分类是再往前的所有段。
		seasonIdx := -1
		for i, s := range segs {
			if reSeasonDir.MatchString(s) {
				seasonIdx = i
				break
			}
		}
		show, cats := "", []string(nil)
		if seasonIdx > 0 {
			show = collectionTitle(segs[seasonIdx-1])
			cats = segs[:seasonIdx-1]
		}
		if show == "" {
			show = strings.TrimSpace(title)
		}
		return MediaInfo{
			Kind:       KindEpisode,
			Categories: cats,
			Title:      show,
			Season:     season,
			Episode:    episode,
		}
	}

	// —— 剧集（无季目录，文件名含 SxxEyy）—— //
	if len(segs) >= 2 {
		base := segs[len(segs)-1]
		if ext := path.Ext(base); ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		if m := reEpisodeName.FindStringSubmatch(base); m != nil {
			season, _ := strconv.Atoi(m[1])
			episode, _ := strconv.Atoi(m[2])
			return MediaInfo{
				Kind:       KindEpisode,
				Categories: segs[:len(segs)-2],
				Title:      showFromParents(segs),
				Season:     season,
				Episode:    episode,
			}
		}
	}

	// —— 中文「第 N 集/话」—— //
	if len(segs) >= 2 {
		base := segs[len(segs)-1]
		if ext := path.Ext(base); ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		if m := reFractionalEpisode.FindStringSubmatch(base); m != nil {
			major, _ := strconv.Atoi(m[1])
			minor, _ := strconv.Atoi(m[2])
			info := episodeMediaInfo(segs, major*10+minor)
			info.Season = 0
			return info
		}
		if m := reChineseEpisode.FindStringSubmatch(base); m != nil {
			episode, _ := strconv.Atoi(m[1])
			return episodeMediaInfo(segs, episode)
		}
	}

	// —— 动漫目录中的尾号分集 —— //
	if len(segs) >= 2 && animationSegments(segs[:len(segs)-1]) {
		base := segs[len(segs)-1]
		if ext := path.Ext(base); ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		// "01 - 0" 这类文件的开头才是实际集号，末尾可能是源站内部序号。
		if m := reLeadingEpisode.FindStringSubmatch(base); m != nil {
			episode, _ := strconv.Atoi(m[1])
			return episodeMediaInfo(segs, episode)
		}
		if m := reTrailingEpisode.FindStringSubmatch(base); m != nil {
			episode, _ := strconv.Atoi(m[1])
			return episodeMediaInfo(segs, episode)
		}
		for _, re := range []*regexp.Regexp{reBareChineseEpisode, reDownloadEpisode, reTaggedEpisode} {
			if m := re.FindStringSubmatch(base); m != nil {
				episode, _ := strconv.Atoi(m[1])
				return episodeMediaInfo(segs, episode)
			}
		}
	}

	// —— 剧集（父目录带季号，文件名以集号结尾）—— //
	// 例：妖怪名单 第一季/妖怪名单 第一季 01.ts、尸兄 第一季/尸兄-10.flv。
	if len(segs) >= 2 {
		parent := segs[len(segs)-2]
		if season, show, ok := namedSeason(parent); ok {
			base := segs[len(segs)-1]
			if ext := path.Ext(base); ext != "" {
				base = strings.TrimSuffix(base, ext)
			}
			if m := reTrailingEpisode.FindStringSubmatch(base); m != nil {
				episode, _ := strconv.Atoi(m[1])
				return MediaInfo{Kind: KindEpisode, Categories: segs[:len(segs)-2], Title: show, Season: season, Episode: episode}
			}
		}
	}

	// —— 剧集(无 Sxx 目录)：文件名是纯数字/第N集，父目录即剧名 —— //
	// 例：/其它/开放式婚姻/03.mp4 → 剧名「开放式婚姻」第 3 集。
	// 这类命名若按电影处理，Jellyfin 会拿「开放式婚姻 03」去 TMDB 搜，
	// 数字乱匹配成不相干的电影（实测被刮成「钢铁侠3」等）。
	if len(segs) >= 2 {
		base := segs[len(segs)-1]
		if ext := path.Ext(base); ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		if ep, ok := plainEpisodeNo(base); ok {
			return episodeMediaInfo(segs, ep)
		}
	}

	// —— 电影：文件名去扩展名作片名，之前的目录段是分类 —— //
	name := title
	var cats []string
	if len(segs) > 0 {
		base := segs[len(segs)-1]
		if ext := path.Ext(base); ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		if base != "" {
			name = base
		}
		cats = segs[:len(segs)-1]
	}
	year := 0
	if ys := reYear.FindString(name); ys != "" {
		year, _ = strconv.Atoi(ys)
	}
	return MediaInfo{
		Kind:       KindMovie,
		Categories: cats,
		Title:      strings.TrimSpace(name),
		Year:       year,
	}
}

func cleanShowDir(s string) string {
	return strings.TrimSpace(reLeadingIndex.ReplaceAllString(strings.TrimSpace(s), ""))
}

func collectionTitle(s string) string {
	s = reLeadingYear.ReplaceAllString(strings.TrimSpace(s), "")
	s = cleanShowDir(s)
	if i := strings.IndexByte(s, '.'); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func showFromParents(segs []string) string {
	for i := len(segs) - 2; i >= 0; i-- {
		if _, show, ok := namedSeason(segs[i]); ok {
			return show
		}
		show := collectionTitle(segs[i])
		if show != "" && !plainNumber(show) && !categoryOnly(show) && !supplementalDir(show) {
			return show
		}
	}
	return ""
}

func seasonFromParents(segs []string) int {
	if len(segs) >= 2 && supplementalDir(segs[len(segs)-2]) {
		return 0
	}
	for i := len(segs) - 2; i >= 0; i-- {
		if season, _, ok := namedSeason(segs[i]); ok {
			return season
		}
		if n, err := strconv.Atoi(strings.TrimSpace(segs[i])); err == nil && n > 0 && n <= 100 {
			return n
		}
	}
	return 1
}

func episodeMediaInfo(segs []string, episode int) MediaInfo {
	return MediaInfo{
		Kind:       KindEpisode,
		Categories: segs[:len(segs)-2],
		Title:      showFromParents(segs),
		Season:     seasonFromParents(segs),
		Episode:    supplementalEpisode(segs, episode),
	}
}

func supplementalDir(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "movie", "movies", "电影版", "剧场版", "番外", "特别篇", "special", "specials":
		return true
	}
	return false
}

func supplementalEpisode(segs []string, episode int) int {
	if len(segs) < 2 {
		return episode
	}
	switch strings.ToLower(strings.TrimSpace(segs[len(segs)-2])) {
	case "番外", "特别篇", "special", "specials":
		return 100 + episode
	default:
		return episode
	}
}

func plainNumber(s string) bool { _, err := strconv.Atoi(strings.TrimSpace(s)); return err == nil }
func categoryOnly(s string) bool {
	for _, marker := range []string{"国漫", "动漫", "动画", "番剧"} {
		if s == marker {
			return true
		}
	}
	return false
}
func animationSegments(segs []string) bool {
	for _, s := range segs {
		for _, marker := range []string{"国漫", "动漫", "动画", "番剧"} {
			if strings.Contains(s, marker) {
				return true
			}
		}
	}
	return false
}

func namedSeason(parent string) (season int, show string, ok bool) {
	m := reNamedSeason.FindStringSubmatch(parent)
	if m == nil {
		return 0, "", false
	}
	if m[1] != "" {
		season = chineseNumber(m[1])
	} else {
		season, _ = strconv.Atoi(m[2])
	}
	if season <= 0 {
		return 0, "", false
	}
	// 季号之后常跟副标题；优先使用季号之前的稳定剧名，让多季归入同一部剧。
	idx := reNamedSeason.FindStringIndex(parent)
	show = cleanShowDir(parent[:idx[0]])
	return season, show, show != ""
}

func chineseNumber(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	digit := map[rune]int{'一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	runes := []rune(s)
	if len(runes) == 1 {
		if runes[0] == '十' {
			return 10
		}
		return digit[runes[0]]
	}
	if len(runes) == 2 {
		if runes[0] == '十' {
			return 10 + digit[runes[1]]
		}
		if runes[1] == '十' {
			return digit[runes[0]] * 10
		}
	}
	if len(runes) == 3 && runes[1] == '十' {
		return digit[runes[0]]*10 + digit[runes[2]]
	}
	return 0
}

// splitClean 把路径切成非空段。
func splitClean(p string) []string {
	raw := strings.Split(p, "/")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// rePlainEp 匹配纯数字或「第N集」形式的分集文件名。
var rePlainEp = regexp.MustCompile(`^(?:第)?(\d{1,3})(?:集|话|話)?$`)

// plainEpisodeNo 判断文件名是否是「纯数字分集」，是则返回集号。
func plainEpisodeNo(base string) (int, bool) {
	m := rePlainEp.FindStringSubmatch(strings.TrimSpace(base))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
