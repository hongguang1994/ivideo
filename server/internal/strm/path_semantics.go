package strm

import (
	"path"
	"regexp"
	"strings"
)

type PathSegmentRole string

const (
	PathRoleCategory  PathSegmentRole = "category"
	PathRoleTitle     PathSegmentRole = "title"
	PathRoleSeason    PathSegmentRole = "season"
	PathRoleMarker    PathSegmentRole = "marker"
	PathRoleTechnical PathSegmentRole = "technical"
	PathRoleFile      PathSegmentRole = "file"
	PathRoleUnknown   PathSegmentRole = "unknown"
)

type PathSegment struct {
	Raw   string          `json:"raw"`
	Clean string          `json:"clean"`
	Role  PathSegmentRole `json:"role"`
	Index int             `json:"index"`
}

type PathToken struct {
	Value   string `json:"value"`
	Kind    string `json:"kind"`
	Segment int    `json:"segment"`
}

// PathAnalysis is the single semantic view shared by grouping, metadata
// matching and STRM layout. Raw segments are retained for explainability.
type PathAnalysis struct {
	Info            MediaInfo        `json:"info"`
	Segments        []PathSegment    `json:"segments"`
	Tokens          []PathToken      `json:"tokens"`
	Candidates      []TitleCandidate `json:"candidates"`
	SpecificTitle   string           `json:"specificTitle"`
	SeriesTitle     string           `json:"seriesTitle"`
	CollectionTitle string           `json:"collectionTitle"`
}

var (
	semanticYear    = regexp.MustCompile(`(?:19|20)\d{2}`)
	semanticQuality = regexp.MustCompile(`(?i)(?:4k|8k|2160p|1080p|720p|hdr|remux|web-?dl|bluray|x26[45]|h26[45])`)
)

func AnalyzeMediaPath(filePath, fallback string) PathAnalysis {
	return analyzeMediaPathWithInfo(filePath, fallback, ParsePath(filePath, fallback))
}

func MediaTitleCandidates(filePath, fallback string, info MediaInfo) []TitleCandidate {
	return analyzeMediaPathWithInfo(filePath, fallback, info).Candidates
}

func analyzeMediaPathWithInfo(filePath, fallback string, info MediaInfo) PathAnalysis {
	analysis := PathAnalysis{Info: info}
	segments := splitClean(filePath)
	for i, raw := range segments {
		clean := strings.TrimSpace(raw)
		role := classifyPathSegment(clean, i == len(segments)-1)
		analysis.Segments = append(analysis.Segments, PathSegment{Raw: raw, Clean: clean, Role: role, Index: i})
		for _, year := range semanticYear.FindAllString(clean, -1) {
			analysis.Tokens = append(analysis.Tokens, PathToken{Value: year, Kind: "year", Segment: i})
		}
		for _, quality := range semanticQuality.FindAllString(clean, -1) {
			analysis.Tokens = append(analysis.Tokens, PathToken{Value: strings.ToLower(quality), Kind: "technical", Segment: i})
		}
		if role == PathRoleSeason {
			analysis.Tokens = append(analysis.Tokens, PathToken{Value: clean, Kind: "season", Segment: i})
		}
	}

	analysis.Candidates = deriveMediaTitleCandidates(filePath, fallback, info)
	analysis.SpecificTitle = preferredSpecificTitle(analysis.Candidates)
	if analysis.SpecificTitle != "" && info.Kind == KindMovie {
		analysis.Info.Title = analysis.SpecificTitle
		for _, candidate := range analysis.Candidates {
			if normalizeCandidateTitle(candidate.Title) == normalizeCandidateTitle(analysis.SpecificTitle) && candidate.Year > 0 {
				analysis.Info.Year = candidate.Year
				break
			}
		}
	}
	if info.Kind == KindEpisode {
		analysis.SeriesTitle = info.Title
	}
	for i := len(analysis.Segments) - 2; i >= 0; i-- {
		segment := analysis.Segments[i]
		if segment.Role != PathRoleTitle {
			continue
		}
		cleaned := collectionTitle(segment.Clean)
		if stronglyObfuscatedTitle(cleaned) {
			cleaned = deobfuscateIndexedTitle(cleaned)
		}
		if plausibleMediaTitle(cleaned) && normalizeCandidateTitle(cleaned) != normalizeCandidateTitle(analysis.SpecificTitle) {
			analysis.CollectionTitle = cleaned
			break
		}
	}
	return analysis
}

func classifyPathSegment(value string, file bool) PathSegmentRole {
	base := value
	if file {
		if ext := path.Ext(base); ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		return PathRoleFile
	}
	if technicalOnlyTitle.MatchString(base) {
		return PathRoleTechnical
	}
	if yearRangeTitle.MatchString(base) || categoryOnly(base) {
		return PathRoleCategory
	}
	if supplementalDir(base) {
		return PathRoleMarker
	}
	if _, _, ok := namedSeason(base); ok {
		return PathRoleSeason
	}
	if plausibleMediaTitle(collectionTitle(base)) {
		return PathRoleTitle
	}
	return PathRoleUnknown
}

func preferredSpecificTitle(candidates []TitleCandidate) string {
	preferred, preferredSource := "", ""
	for _, candidate := range candidates {
		if strings.HasSuffix(candidate.Source, "去混淆") {
			original := strings.TrimSuffix(candidate.Source, "去混淆")
			for _, raw := range candidates {
				indexedParent := raw.Source == "父目录" && raw.Year > 0 && hasLeadingIndexLetter(raw.Title)
				if raw.Source == original && (stronglyObfuscatedTitle(raw.Title) || indexedParent) {
					preferred = candidate.Title
					preferredSource = candidate.Source
					break
				}
			}
		}
	}
	if preferredSource == "父目录去混淆" {
		for _, candidate := range candidates {
			if candidate.Source == "父目录去混淆简化" {
				return candidate.Title
			}
		}
	}
	if len([]rune(preferred)) <= 3 {
		for _, candidate := range candidates {
			if strings.HasPrefix(candidate.Source, "父目录") && strings.HasSuffix(candidate.Source, "简化") && len([]rune(candidate.Title)) > len([]rune(preferred)) {
				return candidate.Title
			}
		}
	}
	if preferred != "" {
		return preferred
	}
	for _, candidate := range candidates {
		if candidate.Source == "解析片名" || candidate.Source == "资源标题" {
			return candidate.Title
		}
	}
	if len(candidates) > 0 {
		return candidates[0].Title
	}
	return ""
}
