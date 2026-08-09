// Package importer owns the share-to-catalog import use case.
package importer

import (
	"context"
	"fmt"
	"path"
	"strings"

	"ivideo/server/internal/eventbus"
	"ivideo/server/internal/mediaevents"
)

type ShareRef struct {
	Provider string
	URL      string
	Password string
	Path     string
	Trigger  string
	// DeferEvent lets batch importers publish one aggregate event after all shares.
	DeferEvent bool
}

type Entry struct {
	Name  string
	Path  string
	IsDir bool
	Size  int64
}

type Resource struct {
	Title    string
	Provider string
	ShareURL string
	SharePwd string
	FilePath string
}

// ShareSource is the storage-provider port used by the importer.
type ShareSource interface {
	Walk(ctx context.Context, share ShareRef) (entries []Entry, supported bool, err error)
	List(ctx context.Context, share ShareRef, subPath string) ([]Entry, error)
}

// ResourceCatalog is the persistence port used by the importer.
type ResourceCatalog interface {
	ExistingKeys(ctx context.Context) (map[string]struct{}, error)
	Add(ctx context.Context, resource Resource) (added bool, err error)
}

type VideoMatcher interface {
	IsVideo(name string) bool
}

type VideoMatcherFunc func(string) bool

func (f VideoMatcherFunc) IsVideo(name string) bool { return f(name) }

type Options struct {
	MaxDepth int
	MaxFiles int
}

type Result struct {
	Added   int
	Skipped int
	Errors  []string
}

type Service struct {
	source  ShareSource
	catalog ResourceCatalog
	videos  VideoMatcher
	options Options
	events  eventbus.Publisher
}

type Option func(*Service)

func WithEvents(events eventbus.Publisher) Option {
	return func(service *Service) { service.events = events }
}

func New(source ShareSource, catalog ResourceCatalog, videos VideoMatcher, options Options, modifiers ...Option) *Service {
	if options.MaxDepth < 0 {
		options.MaxDepth = 0
	}
	if options.MaxFiles <= 0 {
		options.MaxFiles = 1000
	}
	service := &Service{source: source, catalog: catalog, videos: videos, options: options}
	for _, modifier := range modifiers {
		modifier(service)
	}
	return service
}

func (s *Service) Import(ctx context.Context, share ShareRef) (result Result) {
	if s == nil || s.source == nil || s.catalog == nil || s.videos == nil {
		result.Errors = append(result.Errors, "导入模块未完整配置")
		return result
	}
	defer func() {
		if result.Added == 0 || s.events == nil || share.DeferEvent {
			return
		}
		trigger := share.Trigger
		if trigger == "" {
			trigger = "import"
		}
		if err := s.events.Publish(ctx, mediaevents.ResourceImported{
			Trigger: trigger, Provider: share.Provider, ShareURL: share.URL, Added: result.Added,
		}); err != nil {
			result.Errors = append(result.Errors, "触发导入后处理失败: "+err.Error())
		}
	}()
	existing, err := s.catalog.ExistingKeys(ctx)
	if err != nil {
		result.Errors = append(result.Errors, "读取已有资源失败: "+err.Error())
		return result
	}
	add := func(entry Entry) {
		if entry.IsDir || !s.videos.IsVideo(entry.Name) || result.Added >= s.options.MaxFiles {
			return
		}
		key := resourceKey(share.URL, entry.Path)
		if _, ok := existing[key]; ok {
			result.Skipped++
			return
		}
		added, err := s.catalog.Add(ctx, Resource{
			Title:    strings.TrimSuffix(path.Base(entry.Name), path.Ext(entry.Name)),
			Provider: share.Provider, ShareURL: share.URL, SharePwd: share.Password, FilePath: entry.Path,
		})
		if err != nil {
			result.Errors = append(result.Errors, entry.Path+": "+err.Error())
			return
		}
		if !added {
			result.Skipped++
			return
		}
		existing[key] = struct{}{}
		result.Added++
	}

	if entries, supported, walkErr := s.source.Walk(ctx, share); supported {
		if walkErr != nil {
			result.Errors = append(result.Errors, walkErr.Error())
		}
		for _, entry := range entries {
			if ctx.Err() != nil || result.Added >= s.options.MaxFiles {
				break
			}
			add(entry)
		}
		return result
	}

	type node struct {
		path  string
		depth int
	}
	queue := []node{{path: share.Path}}
	for len(queue) > 0 && result.Added < s.options.MaxFiles && ctx.Err() == nil {
		current := queue[0]
		queue = queue[1:]
		entries, listErr := s.source.List(ctx, share, current.path)
		if listErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", current.path, listErr))
			continue
		}
		for _, entry := range entries {
			if entry.IsDir {
				if current.depth < s.options.MaxDepth {
					queue = append(queue, node{path: entry.Path, depth: current.depth + 1})
				}
				continue
			}
			add(entry)
		}
	}
	if err := ctx.Err(); err != nil {
		result.Errors = append(result.Errors, err.Error())
	}
	return result
}

// CompleteBatch publishes one aggregate event after a caller imports several shares.
func (s *Service) CompleteBatch(ctx context.Context, trigger string, added int) error {
	if added <= 0 || s == nil || s.events == nil {
		return nil
	}
	if trigger == "" {
		trigger = "batch import"
	}
	return s.events.Publish(ctx, mediaevents.ResourceImported{Trigger: trigger, Added: added})
}

func resourceKey(shareURL, filePath string) string { return shareURL + "\x00" + filePath }
