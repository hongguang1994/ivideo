package resourcesearch

import (
	"html"
	"net/url"
	"strings"
)

// NormalizeSourceResult is the single boundary between source-specific parsing
// and the stable API model returned by the discovery engine.
func NormalizeSourceResult(raw SourceResult, query string, source SourceDescriptor) (Result, bool) {
	raw.Provider = normalizeProvider(raw.Provider)
	raw.ShareURL = normalizeShareURL(raw.ShareURL)
	if detected := detectProvider(raw.ShareURL); detected != "" {
		raw.Provider = detected
	}
	if raw.Provider == "" || raw.ShareURL == "" {
		return Result{}, false
	}

	raw.SharePwd = strings.TrimSpace(raw.SharePwd)
	if raw.SharePwd == "" {
		raw.SharePwd = passwordFromURL(raw.ShareURL)
	}
	raw.Title = strings.TrimSpace(raw.Title)
	raw.ResourceType = strings.TrimSpace(raw.ResourceType)
	raw.FileName = strings.TrimSpace(raw.FileName)
	raw.UpdatedAt = strings.TrimSpace(raw.UpdatedAt)
	raw.SourceName = strings.TrimSpace(raw.SourceName)
	raw.Repository = strings.TrimSpace(raw.Repository)
	raw.Path = strings.TrimSpace(raw.Path)
	raw.SourceURL = strings.TrimSpace(raw.SourceURL)

	originalTitle := raw.Title
	basis := raw.TitleBasis
	if raw.Title == "" {
		raw.Title = strings.TrimSpace(query)
		basis = TitleBasisQuery
	}
	if basis == "" {
		basis = TitleBasisSource
	}
	raw.TitleBasis = basis
	if raw.SourceName == "" {
		raw.SourceName = source.Name
	}
	raw.Score += source.Priority

	evidence := append([]string(nil), raw.Evidence...)
	evidence = append(evidence, "source:"+source.ID, "title:"+basis, "provider:share-url")
	return Result{
		SourceResult:    raw,
		Source:          source.ID,
		OriginalTitle:   originalTitle,
		TitleConfidence: titleConfidence(raw.Title, query, basis),
		MatchEvidence:   uniqueStrings(evidence),
	}, true
}

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "aliyun", "alipan", "aliyundrive", "ali":
		return "aliyun"
	case "115", "115pan", "pan115":
		return "115"
	case "quark", "quarkpan":
		return "quark"
	default:
		return ""
	}
}

func normalizeShareURL(value string) string {
	value = strings.TrimSpace(html.UnescapeString(value))
	value = strings.TrimRight(value, ".,;，。；")
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func passwordFromURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	for _, key := range []string{"password", "pwd", "code"} {
		if password := strings.TrimSpace(parsed.Query().Get(key)); password != "" {
			return password
		}
	}
	fragment, _ := url.ParseQuery(strings.TrimPrefix(parsed.Fragment, "?"))
	for _, key := range []string{"password", "pwd", "code"} {
		if password := strings.TrimSpace(fragment.Get(key)); password != "" {
			return password
		}
	}
	return ""
}

func titleConfidence(title, query, basis string) int {
	confidence := map[string]int{
		TitleBasisStructured: 95,
		TitleBasisCatalog:    90,
		TitleBasisMessage:    80,
		TitleBasisSource:     70,
		TitleBasisQuery:      40,
	}[basis]
	if confidence == 0 {
		confidence = 60
	}
	if basis != TitleBasisQuery {
		titleKey, queryKey := normalizeSearchText(title), normalizeSearchText(query)
		if queryKey != "" && titleKey == queryKey {
			confidence += 5
		} else if queryKey != "" && strings.Contains(titleKey, queryKey) {
			confidence += 2
		}
	}
	if confidence > 100 {
		return 100
	}
	return confidence
}
