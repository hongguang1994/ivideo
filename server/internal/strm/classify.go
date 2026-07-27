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
	Kind       MediaKind
	Categories []string // 分类段（题材/国别层），如 ["优质韩国电影"] 或 ["小雅动漫每日更新","美国"]
	Title      string   // 电影=片名；剧集=剧名
	Year       int      // 电影年份，0 表示未知
	Season     int      // 剧集季号
	Episode    int      // 剧集集号
}

// Genre 返回用于 Jellyfin 流派标签的分类首段（最粗一层），无则空串。
func (m MediaInfo) Genre() string {
	if len(m.Categories) > 0 {
		return m.Categories[0]
	}
	return ""
}

var (
	// 匹配 .../Sxx/SxxEyy.<ext>：季目录 + 分集文件。大小写不敏感。
	// 季目录下的分集文件：SxxExx 可以**嵌在**文件名任意位置，
	// 如 /S02/癫狂记.Big.Train.S02E06.2002.mkv，不要求整个文件名就是集号。
	reEpisodeFile = regexp.MustCompile(`(?i)/S(\d{1,3})/[^/]*S(\d{1,3})E(\d{1,3})[^/]*\.[a-z0-9]+$`)
	// 从标题里提取 4 位年份（1900–2099）。
	reYear = regexp.MustCompile(`(19|20)\d{2}`)
	// 季目录名 Sxx。
	reSeasonDir = regexp.MustCompile(`(?i)^S(\d{1,3})$`)
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
			show = segs[seasonIdx-1]
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
			return MediaInfo{
				Kind:       KindEpisode,
				Categories: segs[:len(segs)-2],
				Title:      segs[len(segs)-2], // 父目录名即剧名
				Season:     1,
				Episode:    ep,
			}
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
