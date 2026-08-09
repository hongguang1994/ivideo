package resourcesearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	githubAPI         = "https://api.github.com"
	maxGitHubFiles    = 15
	maxGitHubFileSize = 2 << 20
)

var (
	shareURLPattern  = regexp.MustCompile(`https?://(?:www\.)?(?:alipan\.com|aliyundrive\.com|115\.com|pan\.quark\.cn)/s/[A-Za-z0-9_-]+(?:\?[^\s<>"'，。；;）)]*)?`)
	shareCodePattern = regexp.MustCompile(`(?i)(?:提取码|访问码|密码|口令|pwd|code)\s*[:：]?\s*([a-z0-9]{2,12})`)
)

// GitHubProvider 使用 GitHub 官方代码搜索接口检索公开仓库。
type GitHubProvider struct {
	token  string
	client *http.Client
}

func NewGitHub(token string) *GitHubProvider {
	return &GitHubProvider{
		token:  strings.TrimSpace(token),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

type githubSearchResponse struct {
	Items []struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		URL        string `json:"url"`
		HTMLURL    string `json:"html_url"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	} `json:"items"`
	Message string `json:"message"`
}

func (g *GitHubProvider) Search(ctx context.Context, query string) ([]Result, Meta, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, Meta{Source: "github"}, fmt.Errorf("搜索关键词不能为空")
	}
	if g.token == "" {
		return nil, Meta{Source: "github"}, fmt.Errorf("未配置 GitHub Token")
	}

	endpoint := githubAPI + "/search/code?q=" + url.QueryEscape(query+" in:file") + "&per_page=30"
	response, remaining, resetAt, err := g.request(ctx, endpoint, "application/vnd.github+json")
	if err != nil {
		return nil, Meta{Source: "github", Remaining: remaining, ResetAt: resetAt}, err
	}
	defer response.Close()
	var search githubSearchResponse
	if err := json.NewDecoder(response).Decode(&search); err != nil {
		return nil, Meta{Source: "github", Remaining: remaining, ResetAt: resetAt}, fmt.Errorf("解析 GitHub 搜索结果失败: %w", err)
	}

	results := make([]Result, 0)
	seen := make(map[string]bool)
	scanned := 0
	for _, item := range search.Items {
		if scanned >= maxGitHubFiles {
			break
		}
		body, _, _, fetchErr := g.request(ctx, item.URL, "application/vnd.github.raw+json")
		if fetchErr != nil {
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(body, maxGitHubFileSize+1))
		body.Close()
		if readErr != nil || len(content) > maxGitHubFileSize {
			continue
		}
		scanned++
		for _, found := range extractMatchingShareResults(string(content), query) {
			if seen[found.ShareURL] {
				continue
			}
			seen[found.ShareURL] = true
			found.Source = "github"
			found.Repository = item.Repository.FullName
			found.Path = item.Path
			found.SourceURL = item.HTMLURL
			results = append(results, found)
		}
	}
	return results, Meta{Source: "github", Scanned: scanned, Remaining: remaining, ResetAt: resetAt}, nil
}

// CheckToken 通过只读限额接口确认 Token 可被 GitHub 接受。
func (g *GitHubProvider) CheckToken(ctx context.Context) error {
	if g.token == "" {
		return fmt.Errorf("Token 不能为空")
	}
	response, _, _, err := g.request(ctx, githubAPI+"/rate_limit", "application/vnd.github+json")
	if response != nil {
		response.Close()
	}
	return err
}

func (g *GitHubProvider) request(ctx context.Context, endpoint, accept string) (io.ReadCloser, int, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "ivideo-resource-search")
	response, err := g.client.Do(req)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("连接 GitHub 失败: %w", err)
	}
	remaining, _ := strconv.Atoi(response.Header.Get("X-RateLimit-Remaining"))
	resetAt, _ := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&failure)
		if failure.Message == "" {
			failure.Message = response.Status
		}
		if response.StatusCode == http.StatusUnauthorized {
			failure.Message = "GitHub Token 无效"
		} else if remaining == 0 {
			failure.Message = "GitHub 搜索限额已用完，请稍后再试"
		}
		return nil, remaining, resetAt, fmt.Errorf("%s", failure.Message)
	}
	return response.Body, remaining, resetAt, nil
}

func extractShareResults(content, title string) []Result {
	return extractShareResultsWithContext(content, title, false)
}

// extractMatchingShareResults 只提取片名附近的链接，避免代码搜索命中一个大型
// 资源清单后把文件中的其他电影全部误报为当前搜索结果。
func extractMatchingShareResults(content, title string) []Result {
	return extractShareResultsWithContext(content, title, true)
}

func extractShareResultsWithContext(content, title string, requireTitleMatch bool) []Result {
	matches := shareURLPattern.FindAllStringIndex(content, -1)
	results := make([]Result, 0, len(matches))
	titleKey := normalizeSearchText(title)
	for _, match := range matches {
		rawURL := strings.TrimRight(content[match[0]:match[1]], ".,;，。；")
		lineStart := strings.LastIndex(content[:match[0]], "\n") + 1
		lineEnd := len(content)
		if offset := strings.Index(content[match[1]:], "\n"); offset >= 0 {
			lineEnd = match[1] + offset
		}
		lineText := content[lineStart:lineEnd]
		if requireTitleMatch && titleKey != "" && !lineOrNearbyTitleMatches(content, lineStart, lineText, titleKey) {
			continue
		}
		code := ""
		if codeMatch := shareCodePattern.FindStringSubmatch(lineText); len(codeMatch) > 1 {
			code = codeMatch[1]
		}
		results = append(results, Result{
			Provider: detectProvider(rawURL),
			ShareURL: rawURL,
			SharePwd: code,
			Title:    title,
		})
	}
	return results
}

func lineOrNearbyTitleMatches(content string, lineStart int, lineText, titleKey string) bool {
	if strings.Contains(normalizeSearchText(lineText), titleKey) {
		return true
	}
	start := lineStart
	for count := 0; count < 2 && start > 0; count++ {
		previousEnd := start - 1
		previousStart := strings.LastIndex(content[:previousEnd], "\n") + 1
		previousLine := content[previousStart:previousEnd]
		// 遇到上一个资源链接即停止，防止大型表格的相邻行互相命中。
		if shareURLPattern.MatchString(previousLine) {
			return false
		}
		if strings.Contains(normalizeSearchText(previousLine), titleKey) {
			return true
		}
		start = previousStart
	}
	return false
}

func detectProvider(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "alipan.com" || strings.HasSuffix(host, ".alipan.com"), host == "aliyundrive.com" || strings.HasSuffix(host, ".aliyundrive.com"):
		return "aliyun"
	case host == "115.com" || strings.HasSuffix(host, ".115.com"):
		return "115"
	case host == "quark.cn" || strings.HasSuffix(host, ".quark.cn"):
		return "quark"
	default:
		return ""
	}
}
