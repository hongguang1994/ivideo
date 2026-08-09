// Package mediaworkflow coordinates independently replaceable media modules.
package mediaworkflow

import (
	"context"
	"errors"
	"log/slog"

	"ivideo/server/internal/eventbus"
	"ivideo/server/internal/mediaevents"
)

type PublishResult struct {
	Total        int
	Written      int
	Unchanged    int
	Deduplicated int
	Conflicts    int
	Removed      int
	Errors       []string
}

type EnrichmentResult struct {
	ImagePaths []string
}

// Publisher owns generated media artifacts such as STRM and fallback NFO files.
type Publisher interface {
	Publish(ctx context.Context, reason string) (PublishResult, error)
}

// Enricher owns metadata identification, verification, NFO and artwork enrichment.
type Enricher interface {
	Available() bool
	Start(after func(EnrichmentResult, error)) error
}

// Library owns the external media server refresh contract.
type Library interface {
	Refresh(ctx context.Context) error
	RefreshImages(ctx context.Context, paths []string) error
}

type Service struct {
	publisher Publisher
	enricher  Enricher
	library   Library
	events    eventbus.Bus
}

func New(publisher Publisher, enricher Enricher, library Library, events eventbus.Bus) *Service {
	if events == nil {
		events = eventbus.New()
	}
	service := &Service{publisher: publisher, enricher: enricher, library: library, events: events}
	events.Subscribe(mediaevents.ResourceImportedName, service.onResourceImported)
	events.Subscribe(mediaevents.MetadataVerifiedName, service.onMetadataVerified)
	events.Subscribe(mediaevents.MediaPublishedName, service.onMediaPublished)
	return service
}

func (s *Service) Publish(ctx context.Context, reason string) (PublishResult, error) {
	result, err := s.publisher.Publish(ctx, reason)
	if err != nil {
		return result, err
	}
	if err := s.events.Publish(ctx, mediaevents.MediaPublished{
		Trigger: reason, Total: result.Total, Written: result.Written,
		Removed: result.Removed, Unchanged: result.Unchanged,
		Deduplicated: result.Deduplicated, Conflicts: result.Conflicts,
	}); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) StartEnrichment(reason string) {
	if s.enricher == nil || !s.enricher.Available() {
		return
	}
	if err := s.enricher.Start(func(result EnrichmentResult, err error) {
		s.CompleteEnrichment(reason, result, err)
	}); err != nil {
		slog.Debug("元数据任务未启动", "reason", reason, "err", err)
	}
}

func (s *Service) CompleteEnrichment(reason string, result EnrichmentResult, err error) {
	if err != nil {
		slog.Warn("元数据整理失败", "reason", reason, "err", err)
		return
	}
	if publishErr := s.events.Publish(context.Background(), mediaevents.MetadataVerified{
		Trigger: reason, ImagePaths: append([]string(nil), result.ImagePaths...),
	}); publishErr != nil {
		slog.Error("处理元数据完成事件失败", "reason", reason, "err", publishErr)
	}
}

func (s *Service) onResourceImported(ctx context.Context, event eventbus.Event) error {
	imported, ok := event.(mediaevents.ResourceImported)
	if !ok {
		return errors.New("资源导入事件类型不正确")
	}
	slog.Info("收到资源导入事件", "trigger", imported.Trigger, "provider", imported.Provider, "added", imported.Added)
	_, publishErr := s.Publish(ctx, imported.Trigger)
	s.StartEnrichment(imported.Trigger)
	return publishErr
}

func (s *Service) onMetadataVerified(ctx context.Context, event eventbus.Event) error {
	verified, ok := event.(mediaevents.MetadataVerified)
	if !ok {
		return errors.New("元数据完成事件类型不正确")
	}
	slog.Info("收到元数据完成事件", "trigger", verified.Trigger, "imagePaths", len(verified.ImagePaths))
	_, publishErr := s.Publish(ctx, "metadata classification")
	if s.library == nil {
		return publishErr
	}
	var failures []error
	if publishErr != nil {
		failures = append(failures, publishErr)
	}
	if len(verified.ImagePaths) > 0 {
		if err := s.library.RefreshImages(ctx, verified.ImagePaths); err != nil {
			failures = append(failures, err)
		}
	}
	if err := s.library.Refresh(ctx); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (s *Service) onMediaPublished(ctx context.Context, event eventbus.Event) error {
	published, ok := event.(mediaevents.MediaPublished)
	if !ok {
		return errors.New("媒体发布事件类型不正确")
	}
	slog.Info("媒体发布事件", "trigger", published.Trigger, "total", published.Total,
		"written", published.Written, "unchanged", published.Unchanged,
		"deduplicated", published.Deduplicated, "conflicts", published.Conflicts, "removed", published.Removed)
	if s.library == nil || (published.Written == 0 && published.Removed == 0) {
		return nil
	}
	return s.library.Refresh(ctx)
}
