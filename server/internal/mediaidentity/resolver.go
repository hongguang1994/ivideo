// Package mediaidentity combines independently sourced clues into an explainable
// media identity proposal. It performs no network, database or filesystem IO.
package mediaidentity

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const ResolverVersion = "evidence-v1"

type Candidate struct {
	Title        string `json:"title"`
	Year         int    `json:"year,omitempty"`
	Source       string `json:"source"`
	Weight       int    `json:"weight"`
	AutoEligible bool   `json:"autoEligible"`
}

type Input struct {
	SourceTitle    string
	SourceCategory string
	ResourceTitle  string
	FilePath       string
	ParsedKind     string
	ParsedTitle    string
	Season         int
	Episode        int
	Candidates     []Candidate
}

type Evidence struct {
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	Confidence int    `json:"confidence"`
	Reason     string `json:"reason"`
}

type Decision struct {
	Version       string     `json:"version"`
	Status        string     `json:"status"`
	Title         string     `json:"title,omitempty"`
	Kind          string     `json:"kind,omitempty"`
	Season        int        `json:"season,omitempty"`
	Episode       int        `json:"episode,omitempty"`
	Confidence    int        `json:"confidence"`
	Reason        string     `json:"reason"`
	AnimationHint string     `json:"animationHint,omitempty"`
	AutoEligible  bool       `json:"autoEligible"`
	Evidence      []Evidence `json:"evidence"`
}

type Resolver interface {
	Resolve(Input) Decision
}

type DefaultResolver struct{}

var numberedEpisode = regexp.MustCompile(`(?i)^(?:第)?(\d{1,4})(?:集|话|話)?(?:[ ._-]*(?:4k|8k|2160p|1080p|720p|uhd|fhd|hd|hdr))*$`)

func (DefaultResolver) Resolve(input Input) Decision {
	decision := Decision{Version: ResolverVersion, Status: "unresolved", Kind: input.ParsedKind, Season: input.Season, Episode: input.Episode, Evidence: []Evidence{}}
	sourceTitle := cleanTitle(input.SourceTitle)
	if plausibleTitle(sourceTitle) {
		decision.Evidence = append(decision.Evidence, Evidence{
			Kind: "source_context", Value: sourceTitle, Confidence: 82,
			Reason: "资源发布来源明确提供了分享标题",
		})
	}

	bestPath := Candidate{}
	for _, candidate := range input.Candidates {
		if !plausibleTitle(candidate.Title) || candidate.Weight <= bestPath.Weight {
			continue
		}
		bestPath = candidate
	}
	if bestPath.Title != "" {
		decision.Evidence = append(decision.Evidence, Evidence{
			Kind: "path", Value: bestPath.Title, Confidence: min(75, 45+bestPath.Weight*2),
			Reason: "完整路径中存在可解释的片名候选",
		})
	}

	episode, numbered := episodeNumber(input.FilePath)
	if numbered {
		decision.Evidence = append(decision.Evidence, Evidence{
			Kind: "episode_number", Value: strconv.Itoa(episode), Confidence: 90,
			Reason: "文件名由集数和清晰度标记组成",
		})
	}

	if sourceTitle != "" {
		decision.Title = sourceTitle
		decision.Confidence = 82
		decision.Reason = "采用分享来源标题作为作品候选"
		if bestPath.Title != "" && normalize(bestPath.Title) == normalize(sourceTitle) {
			decision.Confidence = 94
			decision.Reason = "分享来源标题与路径标题一致"
			decision.AutoEligible = true
		}
		if numbered {
			decision.Kind = "episode"
			decision.Season = max(1, input.Season)
			decision.Episode = episode
			decision.Confidence = max(decision.Confidence, 92)
			decision.Reason = "分享来源提供作品名，文件名提供明确集数"
			decision.AutoEligible = true
		}
	} else if bestPath.Title != "" {
		decision.Title = cleanTitle(bestPath.Title)
		decision.Confidence = min(80, 45+bestPath.Weight*2)
		decision.Reason = "仅有路径片名证据"
	}

	category := strings.ToLower(strings.TrimSpace(input.SourceCategory))
	switch {
	case containsAny(category, "国漫", "动漫", "动画", "番剧", "anime"):
		decision.AnimationHint = "animation"
		decision.Evidence = append(decision.Evidence, Evidence{Kind: "source_category", Value: input.SourceCategory, Confidence: 85, Reason: "来源分类明确标记为动画"})
	case containsAny(category, "真人", "live action", "live_action"):
		decision.AnimationHint = "live_action"
	}

	switch {
	case decision.Confidence >= 90:
		decision.Status = "identified"
	case decision.Confidence >= 70:
		decision.Status = "candidate"
	}
	return decision
}

func episodeNumber(filePath string) (int, bool) {
	base := path.Base(strings.TrimSpace(filePath))
	base = strings.TrimSuffix(base, path.Ext(base))
	match := numberedEpisode.FindStringSubmatch(strings.TrimSpace(base))
	if len(match) < 2 {
		return 0, false
	}
	episode, err := strconv.Atoi(match[1])
	return episode, err == nil && episode > 0
}

func cleanTitle(value string) string {
	value = strings.TrimSpace(value)
	for _, suffix := range []string{"资源", "网盘资源", "分享"} {
		value = strings.TrimSpace(strings.TrimSuffix(value, suffix))
	}
	return value
}

func plausibleTitle(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 160 {
		return false
	}
	lower := strings.ToLower(value)
	for _, generic := range []string{"资源", "合集", "电影", "剧集", "电视剧", "动漫", "动画", "分享", "movie", "tv", "anime"} {
		if lower == generic {
			return false
		}
	}
	letters := 0
	for _, r := range value {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= 2
}

func normalize(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}

func containsAny(value string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
