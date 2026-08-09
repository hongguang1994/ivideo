// Package githubcollector maintains a small, local index of approved public
// GitHub repositories. It reads share-list text only; it never downloads files
// referenced by a share URL.
package githubcollector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"ivideo/server/internal/resourcesearch"
	"ivideo/server/internal/store"
)

const (
	defaultRepository = "acoooder/aliyunpanshare"
	defaultBranch     = "main"
	defaultParser     = "markdown-table"
	maxTextFileBytes  = 4 << 20
)

type RepositoryStore interface {
	store.GitHubCollectorRepository
}

type TokenProvider func() (string, error)

type Collector struct {
	store   RepositoryStore
	token   TokenProvider
	client  *http.Client
	apiBase string
	rawBase string
}

func New(st RepositoryStore, token TokenProvider) *Collector {
	return &Collector{
		store: st, token: token, client: &http.Client{Timeout: 20 * time.Second},
		apiBase: "https://api.github.com", rawBase: "https://raw.githubusercontent.com",
	}
}

func (c *Collector) EnsureDefaultRepository() error {
	_, err := c.store.EnsureGitHubRepository(defaultRepository, defaultBranch, defaultParser)
	return err
}

type commitResponse struct {
	SHA string `json:"sha"`
}
type treeResponse struct {
	Truncated bool `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
		Size int64  `json:"size"`
	} `json:"tree"`
}

// CollectApproved synchronizes every enabled, approved repository.
func (c *Collector) CollectApproved(ctx context.Context) ([]store.GitHubCollectionResult, error) {
	if err := c.EnsureDefaultRepository(); err != nil {
		return nil, err
	}
	repositories, err := c.store.ListGitHubRepositories()
	if err != nil {
		return nil, err
	}
	results := make([]store.GitHubCollectionResult, 0, len(repositories))
	for _, repo := range repositories {
		if !repo.Enabled {
			continue
		}
		result, err := c.CollectRepository(ctx, repo)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// CollectRepository uses a commit snapshot and Git Tree SHA comparison. Only
// changed Markdown files are fetched from raw.githubusercontent.com.
func (c *Collector) CollectRepository(ctx context.Context, repo store.GitHubRepository) (store.GitHubCollectionResult, error) {
	token, err := c.token()
	if err != nil {
		return store.GitHubCollectionResult{}, err
	}
	if strings.TrimSpace(token) == "" {
		return store.GitHubCollectionResult{}, fmt.Errorf("请先在设置中配置 GitHub Token")
	}
	commit, err := c.fetchCommit(ctx, token, repo.Repository, repo.Branch)
	if err != nil {
		return c.recordFailure(repo, err)
	}
	if commit == repo.LastCommitSHA && commit != "" {
		return c.store.ApplyGitHubRepositorySnapshot(store.GitHubRepositorySnapshot{
			Repository: repo.Repository, Branch: repo.Branch, Parser: repo.Parser, CommitSHA: commit,
		})
	}
	tree, err := c.fetchTree(ctx, token, repo.Repository, commit)
	if err != nil {
		return c.recordFailure(repo, err)
	}
	if tree.Truncated {
		return c.recordFailure(repo, fmt.Errorf("仓库文件树过大，需启用逐级遍历后再采集"))
	}
	existing, err := c.store.GetGitHubRepositoryFiles(repo.ID)
	if err != nil {
		return store.GitHubCollectionResult{}, err
	}
	known := make(map[string]store.GitHubRepositoryFile, len(existing))
	for _, item := range existing {
		known[item.Path] = item
	}
	seen := make(map[string]struct{})
	snapshot := store.GitHubRepositorySnapshot{Repository: repo.Repository, Branch: repo.Branch, Parser: repo.Parser, CommitSHA: commit}
	for _, item := range tree.Tree {
		if !isCollectableMarkdown(item.Path, item.Type, item.Size) {
			continue
		}
		seen[item.Path] = struct{}{}
		if old, found := known[item.Path]; found && old.BlobSHA == item.SHA && old.Active {
			continue
		}
		content, fetchErr := c.fetchRawText(ctx, repo.Repository, commit, item.Path)
		collected := store.GitHubCollectedFile{Path: item.Path, BlobSHA: item.SHA, Size: item.Size}
		if fetchErr != nil {
			collected.ParseError = fetchErr.Error()
		} else {
			for _, parsed := range resourcesearch.ParseMarkdownShareList(content, "", item.Path, repo.Repository, repo.Branch, repo.Repository) {
				collected.Shares = append(collected.Shares, store.GitHubObservedShare{
					Provider: parsed.Provider, ShareURL: parsed.ShareURL, SharePwd: parsed.SharePwd,
					Title: parsed.Title, ResourceType: parsed.ResourceType, FileName: parsed.FileName, UpdatedAt: parsed.UpdatedAt,
					Repository: repo.Repository, Path: item.Path, SourceURL: parsed.SourceURL,
				})
			}
		}
		snapshot.Files = append(snapshot.Files, collected)
	}
	for filePath, old := range known {
		if old.Active {
			if _, found := seen[filePath]; !found {
				snapshot.RemovedPaths = append(snapshot.RemovedPaths, filePath)
			}
		}
	}
	return c.store.ApplyGitHubRepositorySnapshot(snapshot)
}

func isCollectableMarkdown(filePath, typ string, size int64) bool {
	return typ == "blob" && size > 0 && size <= maxTextFileBytes && strings.EqualFold(path.Ext(filePath), ".md")
}

func (c *Collector) recordFailure(repo store.GitHubRepository, collectErr error) (store.GitHubCollectionResult, error) {
	result, err := c.store.ApplyGitHubRepositorySnapshot(store.GitHubRepositorySnapshot{
		Repository: repo.Repository, Branch: repo.Branch, Parser: repo.Parser,
		CommitSHA: repo.LastCommitSHA, CollectionError: collectErr.Error(),
	})
	if err != nil {
		return result, err
	}
	return result, collectErr
}

func (c *Collector) fetchCommit(ctx context.Context, token, repository, branch string) (string, error) {
	var result commitResponse
	if err := c.apiJSON(ctx, token, c.apiBase+"/repos/"+repository+"/commits/"+url.PathEscape(branch), &result); err != nil {
		return "", err
	}
	if result.SHA == "" {
		return "", fmt.Errorf("GitHub 未返回提交 SHA")
	}
	return result.SHA, nil
}

func (c *Collector) fetchTree(ctx context.Context, token, repository, commit string) (treeResponse, error) {
	var result treeResponse
	err := c.apiJSON(ctx, token, c.apiBase+"/repos/"+repository+"/git/trees/"+url.PathEscape(commit)+"?recursive=1", &result)
	return result, err
}

func (c *Collector) apiJSON(ctx context.Context, token, endpoint string, output any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "ivideo-github-collector")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("连接 GitHub 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub 采集请求失败: %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(output)
}

func (c *Collector) fetchRawText(ctx context.Context, repository, commit, filePath string) (string, error) {
	parts := strings.Split(filePath, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	endpoint := c.rawBase + "/" + repository + "/" + commit + "/" + strings.Join(parts, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ivideo-github-collector")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("读取资源清单失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("读取资源清单失败: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTextFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxTextFileBytes {
		return "", fmt.Errorf("资源清单超过大小上限")
	}
	return string(data), nil
}
