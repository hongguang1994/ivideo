package resourcesearch

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CatalogEntry 是 ivideo 已收藏分享与已导入资源的统一搜索投影。
type CatalogEntry struct {
	Provider     string
	ShareURL     string
	SharePwd     string
	Title        string
	Remark       string
	ResourceType string
	FilePath     string
	Status       string
}

// CatalogLoader 将具体数据库实现隔离在资源搜索引擎之外。
type CatalogLoader func(context.Context) ([]CatalogEntry, error)

// CatalogSource 优先检索 ivideo 已经收录的数据，不重复请求外部网站。
type CatalogSource struct {
	load CatalogLoader
}

func NewCatalogSource(load CatalogLoader) *CatalogSource { return &CatalogSource{load: load} }

func (s *CatalogSource) Descriptor() SourceDescriptor {
	return SourceDescriptor{
		ID: "local-catalog", Name: "ivideo 本地索引", Kind: "internal", Priority: 120,
		Description: "检索已收藏分享和已导入资源，不访问外部网站。", Timeout: 5 * time.Second,
	}
}

func (s *CatalogSource) Search(ctx context.Context, query string) ([]SourceResult, Meta, error) {
	queryKey := normalizeSearchText(query)
	if queryKey == "" {
		return []SourceResult{}, Meta{Source: "local-catalog"}, fmt.Errorf("搜索关键词不能为空")
	}
	if s == nil || s.load == nil {
		return []SourceResult{}, Meta{Source: "local-catalog"}, ErrSourceNotConfigured
	}
	entries, err := s.load(ctx)
	if err != nil {
		return []SourceResult{}, Meta{Source: "local-catalog"}, err
	}
	items := make([]SourceResult, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return items, Meta{Source: "local-catalog", Scanned: len(entries)}, err
		}
		if strings.EqualFold(strings.TrimSpace(entry.Status), "invalid") {
			continue
		}
		matchScore, matched := catalogMatchScore(query, entry)
		if !matched {
			continue
		}
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			title = strings.TrimSpace(entry.Remark)
		}
		if title == "" {
			title = strings.TrimSpace(query)
		}
		items = append(items, SourceResult{
			Provider: entry.Provider, ShareURL: entry.ShareURL, SharePwd: entry.SharePwd,
			Title: title, TitleBasis: TitleBasisCatalog, ResourceType: entry.ResourceType, FileName: entry.FilePath,
			SourceName: "ivideo 本地索引", Repository: "ivideo", Path: entry.FilePath,
			Evidence: []string{"catalog:stored-resource"},
			Score:    matchScore,
		})
	}
	return items, Meta{Source: "local-catalog", Scanned: len(entries)}, nil
}
