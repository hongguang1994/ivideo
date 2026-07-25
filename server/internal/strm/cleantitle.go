package strm

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// 片名清理：把网盘里的脏文件名（带分辨率/编码/音轨/字幕/组名等技术标记）
// 洗成适合刮削的干净标题 + 年份。策略保守 —— 只做「剥技术标记 + 取中文主名」，
// 拿不准的年份照常取（个别错的靠 Jellyfin 手动识别修正）。

var (
	// 年份（独立成词的 1900–2099）。
	reYearWord = regexp.MustCompile(`^(19|20)\d{2}$`)
	// 词内的技术标记子串：分辨率 / 来源 / 编码 / 音轨 / 字幕语言 / 组名特征等。
	reTechSub = regexp.MustCompile(`(?i)(` +
		`(bd|hd|hr|web|dvd)?(2160|1080|720|480)[pi]?` + // 分辨率
		`|bluray|blu-ray|bdrip|brrip|bdremux|remux|webrip|web-dl|webdl|hdtv|dvdrip|dvdscr|hdrip|bd-dy|hr-hdtv` + // 来源
		`|x264|x265|h264|h265|hevc|avc|av1|10bit|10bits|8bit` + // 编码
		`|dts-hd|dts|truehd|atmos|ac3|dd5|ddp|aac|flac` + // 音轨
		`|chs|cht|big5|中字|中文字幕|国语|粤语|韩语|日语|英语|双语|中英|国粤|音频|内封|内嵌|` + // 字幕/语言
		`mandarin|cantonese|korean|japanese|english` +
		`|小组|字幕组|压制|影视` + // 组名特征
		`)`)
)

// CleanMovieTitle 从脏文件名提取干净标题和年份（年份 0 表示未取到）。
func CleanMovieTitle(raw string) (title string, year int) {
	// 分隔符（. _ -）→ 空格，再分词。全角标点（：· 等）保留在词内。
	norm := strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(raw)
	words := strings.Fields(norm)

	// 年份：全词扫描取最后一个独立年份词（与技术标记剥离解耦，避免被拆碎的
	// BD-DY/DTS-HD 残留词打断连续剥离）。
	for _, w := range words {
		if reYearWord.MatchString(w) {
			year, _ = strconv.Atoi(w)
		}
	}

	// 标题：含中文时取「从头的连续非纯英文词」，丢掉尾随英文译名；纯英文片则
	// 剥掉技术标记/年份后取剩余。
	title = pickChineseHead(words)
	if title == "" {
		var keep []string
		for _, w := range words {
			if reYearWord.MatchString(w) || isTechToken(w) {
				continue
			}
			keep = append(keep, w)
		}
		title = strings.Join(keep, " ")
	}
	return strings.TrimSpace(title), year
}

// isTechToken 判断一个词是否是纯技术标记：去掉所有技术子串后为空，
// 或是 1~2 位纯数字（多为声道数 5、1 等在分隔后的残留）。
func isTechToken(w string) bool {
	if n := len(w); n >= 1 && n <= 2 && isAllDigit(w) {
		return true
	}
	stripped := reTechSub.ReplaceAllString(w, "")
	stripped = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1 // 去掉残留的 & / 空等
	}, stripped)
	return stripped == ""
}

// pickChineseHead 取词序列开头连续的「含中文或纯数字/标点」词，遇纯英文字母词即停。
func pickChineseHead(words []string) string {
	var head []string
	for _, w := range words {
		if reYearWord.MatchString(w) {
			break // 独立年份词是标题的结束标志
		}
		if hasCJK(w) || !hasLatinLetter(w) {
			head = append(head, w)
			continue
		}
		break
	}
	return strings.Join(head, " ")
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func hasLatinLetter(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func isAllDigit(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
