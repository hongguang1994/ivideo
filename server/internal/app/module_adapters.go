package app

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/config"
	"ivideo/server/internal/eventbus"
	"ivideo/server/internal/handlers"
	"ivideo/server/internal/importer"
	"ivideo/server/internal/jellyfin"
	"ivideo/server/internal/mediaworkflow"
	"ivideo/server/internal/metadata"
	"ivideo/server/internal/resourcesearch"
	"ivideo/server/internal/store"
	"ivideo/server/internal/strm"
)

func buildMediaModules(cfg config.Config, st store.Store, cm *cache.Manager, jf *jellyfin.Client) (*importer.Service, *metadata.Service, *mediaworkflow.Service) {
	// 业务模块只通过事件和窄接口协作。这里是唯一知道所有具体实现的装配点，
	// 替换导入器、元数据源、发布器或媒体服务器时不需要修改模块内部逻辑。
	events := eventbus.New()
	metadataService := metadata.New(st, cfg.MediaDir, cfg.SiteURL)
	importService := importer.New(
		importerShareSource{manager: cm}, importerCatalog{store: st},
		importer.VideoMatcherFunc(func(name string) bool {
			ext := strings.ToLower(path.Ext(name))
			for _, allowed := range cfg.VideoExts {
				if ext == allowed {
					return true
				}
			}
			return false
		}),
		importer.Options{MaxDepth: cfg.ImportMaxDepth, MaxFiles: cfg.ImportMaxFiles}, importer.WithEvents(events),
	)
	var library mediaworkflow.Library
	if jf != nil {
		library = jellyfinLibrary{client: jf}
	}
	workflow := mediaworkflow.New(
		strmPublisher{store: st, cfg: publisherConfig{mediaDir: cfg.MediaDir, siteURL: cfg.SiteURL, mode: cfg.StrmMode}},
		metadataEnricher{service: metadataService}, library, events,
	)
	return importService, metadataService, workflow
}

func buildDiscovery(cfg config.Config, st store.Store, metadataService *metadata.Service) *resourcesearch.Engine {
	// 每个来源是独立插件；引擎统一处理并发、超时、缓存、去重和健康状态。
	engine := resourcesearch.NewEngine(resourcesearch.EngineOptions{
		Concurrency: cfg.DiscoveryConcurrency,
		Timeout:     time.Duration(cfg.DiscoveryTimeoutSeconds) * time.Second,
		CacheTTL:    time.Duration(cfg.DiscoveryCacheMinutes) * time.Minute,
		MaxResults:  cfg.DiscoveryMaxResults,
	})
	engine.Register(resourcesearch.NewCatalogSource(func(ctx context.Context) ([]resourcesearch.CatalogEntry, error) {
		shares, err := st.ListShares()
		if err != nil {
			return nil, err
		}
		resources, err := st.ListResources()
		if err != nil {
			return nil, err
		}
		entries := make([]resourcesearch.CatalogEntry, 0, len(shares)+len(resources))
		for _, share := range shares {
			entries = append(entries, resourcesearch.CatalogEntry{
				Provider: share.Provider, ShareURL: share.ShareURL, SharePwd: share.SharePwd,
				Title: share.Title, Remark: share.Remark, ResourceType: share.Category, Status: share.Status,
			})
		}
		for _, resource := range resources {
			entries = append(entries, resourcesearch.CatalogEntry{
				Provider: resource.Provider, ShareURL: resource.ShareURL, SharePwd: resource.SharePwd,
				Title: resource.Title, FilePath: resource.FilePath,
			})
		}
		return entries, ctx.Err()
	}))
	githubToken := func() (string, error) {
		credential, found, err := st.GetCredential("github")
		if err != nil {
			return "", err
		}
		if !found || strings.TrimSpace(credential.Token) == "" {
			return "", resourcesearch.ErrSourceNotConfigured
		}
		return credential.Token, nil
	}
	engine.Register(resourcesearch.SourceFunc{
		Info: resourcesearch.SourceDescriptor{ID: "aliyunpanshare", Name: "阿里云盘结构化仓库", Priority: 90},
		SearchFunc: func(ctx context.Context, query string) ([]resourcesearch.Result, resourcesearch.Meta, error) {
			token, err := githubToken()
			if err != nil {
				return nil, resourcesearch.Meta{Source: "aliyunpanshare"}, err
			}
			return resourcesearch.NewAliyunPanShare(token).Search(ctx, query)
		},
	})
	engine.Register(resourcesearch.SourceFunc{
		Info: resourcesearch.SourceDescriptor{ID: "github-code", Name: "GitHub 公开仓库", Priority: 65},
		SearchFunc: func(ctx context.Context, query string) ([]resourcesearch.Result, resourcesearch.Meta, error) {
			token, err := githubToken()
			if err != nil {
				return nil, resourcesearch.Meta{Source: "github"}, err
			}
			return resourcesearch.NewGitHub(token).Search(ctx, query)
		},
	})
	engine.Register(resourcesearch.SourceFunc{
		Info: resourcesearch.SourceDescriptor{ID: "metadata-alias", Name: "TMDb 别名扩展", Priority: 75},
		SearchFunc: func(ctx context.Context, query string) ([]resourcesearch.Result, resourcesearch.Meta, error) {
			expanded := metadataService.PreferredDiscoveryQuery(ctx, query)
			if strings.EqualFold(strings.TrimSpace(expanded), strings.TrimSpace(query)) {
				return []resourcesearch.Result{}, resourcesearch.Meta{Source: "metadata-alias"}, nil
			}
			token, err := githubToken()
			if err != nil {
				return nil, resourcesearch.Meta{Source: "metadata-alias"}, err
			}
			return resourcesearch.NewGitHub(token).Search(ctx, expanded)
		},
	})
	engine.Register(resourcesearch.NewTelegramSource(cfg.DiscoveryTelegramChannels))
	return engine
}

type importerShareSource struct{ manager *cache.Manager }

func (a importerShareSource) Walk(ctx context.Context, share importer.ShareRef) ([]importer.Entry, bool, error) {
	entries, supported, err := a.manager.WalkShareContext(ctx, toCacheShare(share))
	return toImporterEntries(entries), supported, err
}

func (a importerShareSource) List(ctx context.Context, share importer.ShareRef, subPath string) ([]importer.Entry, error) {
	entries, err := a.manager.ListShareContext(ctx, toCacheShare(share), subPath)
	return toImporterEntries(entries), err
}

func toCacheShare(share importer.ShareRef) cache.ShareRef {
	return cache.ShareRef{Provider: share.Provider, ShareURL: share.URL, SharePwd: share.Password, FilePath: share.Path}
}

func toImporterEntries(entries []cache.ShareEntry) []importer.Entry {
	result := make([]importer.Entry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, importer.Entry{Name: entry.Name, Path: entry.Path, IsDir: entry.IsDir, Size: entry.Size})
	}
	return result
}

type importerCatalog struct{ store store.ResourceRepository }

func (a importerCatalog) ExistingKeys(ctx context.Context) (map[string]struct{}, error) {
	resources, err := a.store.ListResources()
	if err != nil {
		return nil, err
	}
	keys := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		keys[resource.ShareURL+"\x00"+resource.FilePath] = struct{}{}
	}
	return keys, ctx.Err()
}

func (a importerCatalog) Add(ctx context.Context, resource importer.Resource) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err := a.store.AddResource(store.Resource{
		Title: resource.Title, Provider: resource.Provider, ShareURL: resource.ShareURL,
		SharePwd: resource.SharePwd, FilePath: resource.FilePath,
	})
	if errors.Is(err, store.ErrResourceExists) {
		return false, nil
	}
	return err == nil, err
}

type strmPublisher struct {
	store strm.Repository
	cfg   publisherConfig
}

type publisherConfig struct {
	mediaDir string
	siteURL  string
	mode     string
}

func (a strmPublisher) Publish(ctx context.Context, _ string) (mediaworkflow.PublishResult, error) {
	if err := ctx.Err(); err != nil {
		return mediaworkflow.PublishResult{}, err
	}
	result, err := strm.New(a.store, a.cfg.mediaDir, a.cfg.siteURL, a.cfg.mode, handlers.APIPrefix).Generate()
	return mediaworkflow.PublishResult{
		Total: result.Total, Written: result.Written, Unchanged: result.Unchanged,
		Deduplicated: result.Deduplicated, Conflicts: result.Conflicts,
		Removed: result.Removed, Errors: result.Errors,
	}, err
}

type metadataEnricher struct{ service *metadata.Service }

func (a metadataEnricher) Available() bool {
	status, err := a.service.Status()
	return err == nil && status.Configured && !status.Running
}

func (a metadataEnricher) Start(after func(mediaworkflow.EnrichmentResult, error)) error {
	return a.service.Start(func(result metadata.Result, err error) {
		after(mediaworkflow.EnrichmentResult{ImagePaths: result.ImagePaths}, err)
	})
}

type jellyfinLibrary struct{ client *jellyfin.Client }

func (a jellyfinLibrary) Refresh(context.Context) error { return a.client.RefreshLibrary() }

func (a jellyfinLibrary) RefreshImages(_ context.Context, paths []string) error {
	_, err := a.client.RefreshImagesByPaths(paths)
	return err
}
