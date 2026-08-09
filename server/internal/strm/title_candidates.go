package strm

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// TitleCandidate records one independently derived title clue. Weight describes
// the reliability of the location, not whether a metadata provider matched it.
type TitleCandidate struct {
	Title        string
	Year         int
	Source       string
	Weight       int
	AutoEligible bool
}

var (
	technicalOnlyTitle = regexp.MustCompile(`(?i)^(?:4k|8k|uhd|fhd|hd|hdr|hq|sd|720p|1080p|2160p|web(?:-?dl)?|bluray|remux|x26[45]|h26[45])(?:[ ._()（）-]*(?:国语|粤语|普通话|英语|中字|上集|下集|版|60帧|dolby|dts))*$`)
	yearRangeTitle     = regexp.MustCompile(`^(?:19|20)\d{2}\s*[-~至]\s*(?:19|20)\d{2}$`)
	trailingPartNumber = regexp.MustCompile(`(?i)^(.+?)(?:第)?\s*([1-9]\d?)\s*(?:季|部)?$`)
	editionSuffix      = regexp.MustCompile(`(?:中国版|大陆版|国版)$`)
)

var titleQualifierWords = map[string]bool{
	"国产": true, "中国大陆": true, "中国": true, "大陆": true, "香港": true, "台湾": true,
	"美国": true, "英国": true, "法国": true, "德国": true, "印度": true, "日本": true, "韩国": true,
	"动作": true, "剧情": true, "喜剧": true, "爱情": true, "科幻": true, "奇幻": true, "悬疑": true,
	"惊悚": true, "恐怖": true, "犯罪": true, "战争": true, "历史": true, "古装": true, "动画": true,
}

// MediaTitleCandidates derives conservative search titles from independent
// path levels. A provider must still validate a candidate before publication.
func deriveMediaTitleCandidates(filePath, fallback string, info MediaInfo) []TitleCandidate {
	var out []TitleCandidate
	seen := map[string]bool{}
	add := func(raw, source string, weight int, autoEligible bool) {
		title, year := CleanMovieTitle(strings.TrimSpace(raw))
		if info.Kind == KindEpisode {
			title = cleanSeriesCandidate(raw)
			year = 0
		}
		if !plausibleMediaTitle(title) {
			return
		}
		key := normalizeCandidateTitle(title)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, TitleCandidate{Title: title, Year: year, Source: source, Weight: weight, AutoEligible: autoEligible})
	}
	addWithDeobfuscated := func(raw, source string, weight int, autoEligible bool) {
		add(raw, source, weight, autoEligible)
		cleaned := deobfuscateIndexedTitle(raw)
		if cleaned != strings.TrimSpace(raw) {
			add(cleaned, source+"去混淆", max(0, weight-1), autoEligible)
		}
		cleanTitle, cleanYear := CleanMovieTitle(cleaned)
		if simplified := stripTitleQualifiers(cleanTitle); simplified != strings.TrimSpace(cleanTitle) {
			if cleanYear > 0 {
				simplified += " " + strconv.Itoa(cleanYear)
			}
			add(simplified, source+"去混淆简化", max(0, weight-2), autoEligible)
		}
	}

	addWithDeobfuscated(info.Title, "解析片名", 12, true)
	addWithDeobfuscated(fallback, "资源标题", 10, true)
	primaryTitle := ""
	if len(out) > 0 {
		primaryTitle = out[0].Title
	}
	segments := splitClean(filePath)
	if len(segments) > 0 {
		segments = segments[:len(segments)-1]
	}
	parentRank := 0
	for i := len(segments) - 1; i >= 0 && parentRank < 4; i-- {
		raw := strings.TrimSpace(segments[i])
		if supplementalDir(raw) || categoryOnly(raw) || !plausibleMediaTitle(raw) {
			continue
		}
		cleaned := collectionTitle(raw)
		weight := 15 - parentRank*2
		autoEligible := primaryTitle == "" && parentRank == 0
		if info.Kind == KindEpisode && isNumberedPartOf(primaryTitle, cleaned) {
			autoEligible = true
		}
		addWithDeobfuscated(cleaned, "父目录", weight, autoEligible)
		parentRank++
	}
	return out
}

func stripTitleQualifiers(value string) string {
	value = strings.TrimSpace(value)
	value = editionSuffix.ReplaceAllString(value, "")
	parts := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(value))
	for i := 1; i < len(parts); i++ {
		if titleQualifierWords[parts[i]] {
			parts = parts[:i]
			break
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// deobfuscateIndexedTitle derives an additional search clue for collections
// that prefix folders with a pinyin index letter and insert initials between
// Han characters, such as "Y英X雄S三Y元L里2026". The original candidate is
// always retained and searched first, so legitimate mixed titles like X战警
// are not silently renamed.
func deobfuscateIndexedTitle(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) < 2 {
		return strings.TrimSpace(value)
	}
	start := 0
	for start < len(runes) && start < 2 && isUpperASCII(runes[start]) {
		start++
	}
	if start == 0 || start >= len(runes) || !unicode.Is(unicode.Han, runes[start]) {
		start = 0
	}
	runes = runes[start:]
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		if isUpperASCII(r) && i > 0 && i+1 < len(runes) && unicode.Is(unicode.Han, runes[i-1]) && unicode.Is(unicode.Han, runes[i+1]) {
			continue
		}
		out = append(out, r)
	}
	return strings.TrimSpace(string(out))
}

func stronglyObfuscatedTitle(value string) bool {
	runes := []rune(strings.TrimSpace(value))
	leading, internal := 0, 0
	for leading < len(runes) && leading < 2 && isUpperASCII(runes[leading]) {
		leading++
	}
	if leading >= len(runes) || !unicode.Is(unicode.Han, runes[leading]) {
		leading = 0
	}
	for i, r := range runes {
		if isUpperASCII(r) && i > 0 && i+1 < len(runes) && unicode.Is(unicode.Han, runes[i-1]) && unicode.Is(unicode.Han, runes[i+1]) {
			internal++
		}
	}
	compactYear := reCJKTrailingYear.MatchString(strings.TrimSpace(value))
	return internal >= 2 || (leading > 0 && (internal > 0 || compactYear))
}

func hasLeadingIndexLetter(value string) bool {
	runes := []rune(strings.TrimSpace(value))
	i := 0
	for i < len(runes) && i < 2 && isUpperASCII(runes[i]) {
		i++
	}
	return i > 0 && i < len(runes) && unicode.Is(unicode.Han, runes[i])
}

func isUpperASCII(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

func isNumberedPartOf(child, parent string) bool {
	match := trailingPartNumber.FindStringSubmatch(strings.TrimSpace(child))
	if len(match) == 0 {
		return false
	}
	return normalizeCandidateTitle(match[1]) == normalizeCandidateTitle(parent)
}

func cleanSeriesCandidate(raw string) string {
	raw = strings.TrimSpace(raw)
	if ext := path.Ext(raw); ext != "" && len(ext) <= 6 {
		raw = strings.TrimSuffix(raw, ext)
	}
	if _, show, ok := namedSeason(raw); ok {
		return show
	}
	return collectionTitle(raw)
}

func plausibleMediaTitle(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || technicalOnlyTitle.MatchString(value) || yearRangeTitle.MatchString(value) {
		return false
	}
	lower := strings.ToLower(value)
	switch lower {
	case "movie", "movies", "tv", "anime", "video", "videos", "电影", "电视剧", "剧集", "资源", "合集", "国漫", "动漫", "动画", "番剧":
		return false
	}
	letters := 0
	for _, r := range value {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= 2
}

func normalizeCandidateTitle(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}
