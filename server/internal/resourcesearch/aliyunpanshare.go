package resourcesearch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const aliyunPanShareRepo = "acoooder/aliyunpanshare"

const (
	maxAliyunPanShareFiles     = 40
	maxAliyunPanShareFileBytes = 4 << 20
	aliyunPanShareSourceName   = "阿里云盘分享库"
)

type repositoryTreeResponse struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
}

type repositoryContentResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type repositoryCodeSearchResponse struct {
	Items []struct {
		Path string `json:"path"`
	} `json:"items"`
}

// AliyunPanShareProvider 解析 acoooder/aliyunpanshare 中的 Markdown 资源表格。
// 这个仓库的资源是结构化表格，不能只依赖通用的 URL 正则，否则会丢失资源名、文件名和更新时间。
type AliyunPanShareProvider struct {
	github *GitHubProvider
}

func NewAliyunPanShare(token string) *AliyunPanShareProvider {
	return &AliyunPanShareProvider{github: NewGitHub(token)}
}

func (p *AliyunPanShareProvider) Search(ctx context.Context, query string) ([]SourceResult, Meta, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, Meta{Source: "aliyunpanshare"}, fmt.Errorf("搜索关键词不能为空")
	}
	if p.github.token == "" {
		return nil, Meta{Source: "aliyunpanshare"}, fmt.Errorf("未配置 GitHub Token")
	}

	treeBody, remaining, resetAt, err := p.github.request(ctx, githubAPI+"/repos/"+aliyunPanShareRepo+"/git/trees/main?recursive=1", "application/vnd.github+json")
	if err != nil {
		return nil, Meta{Source: "aliyunpanshare", Remaining: remaining, ResetAt: resetAt}, err
	}
	defer treeBody.Close()
	var tree repositoryTreeResponse
	if err := json.NewDecoder(treeBody).Decode(&tree); err != nil {
		return nil, Meta{Source: "aliyunpanshare", Remaining: remaining, ResetAt: resetAt}, fmt.Errorf("解析资源库目录失败: %w", err)
	}

	paths := make([]string, 0)
	for _, item := range tree.Tree {
		if item.Type == "blob" && strings.HasSuffix(strings.ToLower(item.Path), ".md") {
			paths = append(paths, item.Path)
		}
	}
	paths = prioritizeCurrentFiles(paths)
	results, scanned := p.scanMarkdownFiles(ctx, paths, query)
	if len(results) == 0 {
		// 历史记录数量很大，不逐个下载；让 GitHub 先定位包含关键词的文件。
		matchedPaths, searchErr := p.findRepositoryPaths(ctx, query)
		if searchErr == nil {
			results, scanned = p.scanMarkdownFiles(ctx, matchedPaths, query)
		}
	}
	return results, Meta{Source: "aliyunpanshare", Scanned: scanned, Remaining: remaining, ResetAt: resetAt}, nil
}

// List 返回资源源当前合集中的全部表格资源，不需要关键词。
func (p *AliyunPanShareProvider) List(ctx context.Context) ([]SourceResult, Meta, error) {
	if p.github.token == "" {
		return nil, Meta{Source: "aliyunpanshare"}, fmt.Errorf("未配置 GitHub Token")
	}
	treeBody, remaining, resetAt, err := p.github.request(ctx, githubAPI+"/repos/"+aliyunPanShareRepo+"/git/trees/main?recursive=1", "application/vnd.github+json")
	if err != nil {
		return nil, Meta{Source: "aliyunpanshare", Remaining: remaining, ResetAt: resetAt}, err
	}
	defer treeBody.Close()
	var tree repositoryTreeResponse
	if err := json.NewDecoder(treeBody).Decode(&tree); err != nil {
		return nil, Meta{Source: "aliyunpanshare", Remaining: remaining, ResetAt: resetAt}, err
	}
	paths := make([]string, 0)
	for _, item := range tree.Tree {
		if item.Type == "blob" && strings.HasSuffix(strings.ToLower(item.Path), ".md") && !strings.Contains(item.Path, "/") {
			paths = append(paths, item.Path)
		}
	}
	sort.Strings(paths)
	results := make([]SourceResult, 0)
	scanned := 0
	for _, filePath := range paths {
		content, fetchErr := p.github.fetchRepositoryFile(ctx, filePath)
		if fetchErr != nil || len(content) > maxAliyunPanShareFileBytes {
			continue
		}
		scanned++
		results = append(results, parseAliyunPanShareMarkdown(content, "", filePath)...)
	}
	return results, Meta{Source: "aliyunpanshare", Scanned: scanned, Remaining: remaining, ResetAt: resetAt}, nil
}

func prioritizeCurrentFiles(paths []string) []string {
	sort.Strings(paths)
	current := make([]string, 0, len(paths))
	history := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.Contains(path, "/") {
			history = append(history, path)
		} else {
			current = append(current, path)
		}
	}
	ordered := append(current, history...)
	if len(ordered) > maxAliyunPanShareFiles {
		ordered = ordered[:maxAliyunPanShareFiles]
	}
	return ordered
}

func (p *AliyunPanShareProvider) scanMarkdownFiles(ctx context.Context, paths []string, query string) ([]SourceResult, int) {
	results := make([]SourceResult, 0)
	seen := make(map[string]int)
	scanned := 0
	for _, path := range paths {
		content, fetchErr := p.github.fetchRepositoryFile(ctx, path)
		if fetchErr != nil || len(content) > maxAliyunPanShareFileBytes {
			continue
		}
		scanned++
		for _, found := range parseAliyunPanShareMarkdown(content, query, path) {
			if index, ok := seen[found.ShareURL]; ok {
				results[index] = mergeResourceResult(results[index], found)
				continue
			}
			seen[found.ShareURL] = len(results)
			results = append(results, found)
		}
	}
	return results, scanned
}

func (p *AliyunPanShareProvider) findRepositoryPaths(ctx context.Context, query string) ([]string, error) {
	endpoint := githubAPI + "/search/code?q=" + url.QueryEscape(query+" repo:"+aliyunPanShareRepo+" in:file") + "&per_page=30"
	body, _, _, err := p.github.request(ctx, endpoint, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var response repositoryCodeSearchResponse
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(response.Items))
	for _, item := range response.Items {
		if strings.HasSuffix(strings.ToLower(item.Path), ".md") {
			paths = append(paths, item.Path)
		}
	}
	return prioritizeCurrentFiles(paths), nil
}

func (g *GitHubProvider) fetchRepositoryFile(ctx context.Context, filePath string) (string, error) {
	endpoint := githubAPI + "/repos/" + aliyunPanShareRepo + "/contents/" + url.PathEscape(filePath) + "?ref=main"
	body, _, _, err := g.request(ctx, endpoint, "application/vnd.github+json")
	if err != nil {
		return "", err
	}
	defer body.Close()
	var file repositoryContentResponse
	if err := json.NewDecoder(body).Decode(&file); err != nil {
		return "", err
	}
	if file.Encoding != "base64" {
		return "", fmt.Errorf("不支持的 GitHub 文件编码: %s", file.Encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	return string(decoded), err
}

var markdownLinkPattern = regexp.MustCompile(`https?://(?:www\.)?(?:alipan\.com|aliyundrive\.com|115\.com|pan\.quark\.cn)/s/[A-Za-z0-9_-]+(?:\?[^\s<>"'，。；;）)]*)?`)

func parseAliyunPanShareMarkdown(content, query, filePath string) []SourceResult {
	return ParseMarkdownShareList(content, query, filePath, aliyunPanShareRepo, "main", aliyunPanShareSourceName)
}

// ParseMarkdownShareList parses a structured Markdown share list. The parser is
// intentionally repository-neutral so the collection module can reuse it for
// every approved GitHub source without copying provider-specific rules.
func ParseMarkdownShareList(content, query, filePath, repository, branch, sourceName string) []SourceResult {
	queryLower := strings.ToLower(strings.TrimSpace(query))
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	results := make([]SourceResult, 0)
	var headers map[string]int
	for _, line := range lines {
		cells := splitMarkdownRow(line)
		if len(cells) < 2 || isMarkdownSeparator(cells) {
			continue
		}
		clean := make([]string, len(cells))
		for i := range cells {
			clean[i] = cleanMarkdownCell(cells[i])
		}
		if detected := markdownHeaders(clean); len(detected) > 0 {
			headers = detected
			continue
		}
		if len(headers) == 0 {
			continue
		}
		links := markdownLinkPattern.FindAllString(line, -1)
		if len(links) == 0 {
			continue
		}
		resourceType, title, fileName, updatedAt, sharePwd := markdownResourceFields(clean, headers)
		searchText := strings.ToLower(strings.Join(clean, " "))
		if queryLower != "" && !strings.Contains(searchText, queryLower) {
			continue
		}
		for _, rawURL := range links {
			rawURL = strings.TrimRight(rawURL, ".,;，。；")
			results = append(results, SourceResult{
				Provider: detectProvider(rawURL), ShareURL: rawURL, SharePwd: sharePwd, Title: title,
				TitleBasis: TitleBasisStructured, ResourceType: resourceType, FileName: fileName, UpdatedAt: updatedAt,
				SourceName: sourceName, Evidence: []string{"github:structured-row"},
				Repository: repository, Path: filePath,
				SourceURL: "https://github.com/" + repository + "/blob/" + branch + "/" + url.PathEscape(filePath),
			})
		}
	}
	return results
}

func splitMarkdownRow(line string) []string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "|") {
		line = line[1:]
	}
	if strings.HasSuffix(line, "|") {
		line = line[:len(line)-1]
	}
	return strings.Split(line, "|")
}

func isMarkdownSeparator(cells []string) bool {
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" || strings.Trim(cell, ":- ") == "" {
			continue
		}
		return false
	}
	return true
}

func cleanMarkdownCell(cell string) string {
	cell = strings.TrimSpace(cell)
	cell = strings.ReplaceAll(cell, "\\|", "|")
	cell = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(cell, "")
	cell = regexp.MustCompile(`!?(?:\[([^\]]*)\])\([^)]*\)`).ReplaceAllString(cell, "$1")
	return strings.TrimSpace(cell)
}

func markdownHeaders(cells []string) map[string]int {
	headers := make(map[string]int)
	for index, cell := range cells {
		lower := strings.ToLower(strings.TrimSpace(cell))
		switch {
		case strings.Contains(lower, "资源类型") || lower == "类型":
			headers["type"] = index
		case strings.Contains(lower, "资源名称") || lower == "名称":
			headers["title"] = index
		case strings.Contains(lower, "文件名称") || lower == "文件名":
			headers["file"] = index
		case strings.Contains(lower, "更新时间") || strings.Contains(lower, "发布时间"):
			headers["updated"] = index
		case strings.Contains(lower, "提取码") || strings.Contains(lower, "访问码") || strings.Contains(lower, "密码"):
			headers["pwd"] = index
		}
	}
	if _, ok := headers["title"]; !ok {
		return nil
	}
	return headers
}

func markdownResourceFields(cells []string, headers map[string]int) (resourceType, title, fileName, updatedAt, sharePwd string) {
	cell := func(key string) string {
		index, ok := headers[key]
		if !ok || index < 0 || index >= len(cells) {
			return ""
		}
		return cells[index]
	}
	return cell("type"), cell("title"), cell("file"), cell("updated"), cell("pwd")
}

func mergeResourceResult(existing, incoming SourceResult) SourceResult {
	if existing.Title == "" {
		existing.Title = incoming.Title
	}
	if existing.ResourceType == "" {
		existing.ResourceType = incoming.ResourceType
	}
	if existing.FileName == "" {
		existing.FileName = incoming.FileName
	}
	if incoming.UpdatedAt > existing.UpdatedAt {
		existing.UpdatedAt = incoming.UpdatedAt
	}
	return existing
}
