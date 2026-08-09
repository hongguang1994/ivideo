package resourcesearch

import (
	"strings"
	"unicode"
)

// catalogMatchScore keeps typo tolerance conservative: short queries must match
// exactly, while longer queries may differ by a small number of runes.
func catalogMatchScore(query string, entry CatalogEntry) (int, bool) {
	queryKey := normalizeFuzzyText(query)
	if queryKey == "" {
		return 0, false
	}

	fields := []string{entry.Title, entry.Remark, entry.FilePath}
	best := 0
	for _, field := range fields {
		candidate := normalizeFuzzyText(field)
		if candidate == "" {
			continue
		}
		switch {
		case candidate == queryKey:
			if best < 100 {
				best = 100
			}
		case strings.Contains(candidate, queryKey):
			if best < 85 {
				best = 85
			}
		}
	}
	if best > 0 {
		return best, true
	}

	queryRunes := []rune(queryKey)
	if len(queryRunes) < 4 {
		return 0, false
	}

	threshold := fuzzyThreshold(len(queryRunes))
	bestSimilarity := 0.0
	for _, segment := range fuzzySegments(fields...) {
		similarity := bestRuneSimilarity(queryRunes, []rune(segment))
		if similarity > bestSimilarity {
			bestSimilarity = similarity
		}
	}
	if bestSimilarity < threshold {
		return 0, false
	}
	return 40 + int(bestSimilarity*35), true
}

func normalizeFuzzyText(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func fuzzySegments(values ...string) []string {
	seen := make(map[string]struct{})
	segments := make([]string, 0, len(values)*3)
	add := func(value string) {
		value = normalizeFuzzyText(value)
		if len([]rune(value)) < 2 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		segments = append(segments, value)
	}

	for _, value := range values {
		add(value)
		var part strings.Builder
		flush := func() {
			add(part.String())
			part.Reset()
		}
		for _, r := range value {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				part.WriteRune(r)
				continue
			}
			flush()
		}
		flush()
	}
	return segments
}

func fuzzyThreshold(queryLength int) float64 {
	switch {
	case queryLength <= 4:
		return 0.75
	case queryLength <= 7:
		return 0.80
	default:
		return 0.82
	}
}

func bestRuneSimilarity(query, candidate []rune) float64 {
	if len(query) == 0 || len(candidate) == 0 {
		return 0
	}
	if len(candidate) <= len(query)+1 {
		return runeSimilarity(query, candidate)
	}

	best := 0.0
	for _, width := range []int{len(query) - 1, len(query), len(query) + 1} {
		if width < 2 || width > len(candidate) {
			continue
		}
		for start := 0; start+width <= len(candidate); start++ {
			if similarity := runeSimilarity(query, candidate[start:start+width]); similarity > best {
				best = similarity
			}
		}
	}
	return best
}

func runeSimilarity(a, b []rune) float64 {
	maximum := len(a)
	if len(b) > maximum {
		maximum = len(b)
	}
	if maximum == 0 {
		return 1
	}
	return 1 - float64(runeEditDistance(a, b))/float64(maximum)
}

func runeEditDistance(a, b []rune) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for index := range previous {
		previous[index] = index
	}
	for i, left := range a {
		current[0] = i + 1
		for j, right := range b {
			cost := 1
			if left == right {
				cost = 0
			}
			current[j+1] = minInt(
				previous[j+1]+1,
				current[j]+1,
				previous[j]+cost,
			)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func minInt(values ...int) int {
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}
