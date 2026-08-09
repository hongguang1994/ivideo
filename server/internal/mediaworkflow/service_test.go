package mediaworkflow

import (
	"context"
	"testing"

	"ivideo/server/internal/eventbus"
	"ivideo/server/internal/mediaevents"
)

type fakePublisher struct {
	calls   []string
	results []PublishResult
}

func (f *fakePublisher) Publish(_ context.Context, reason string) (PublishResult, error) {
	f.calls = append(f.calls, reason)
	result := PublishResult{}
	if len(f.results) > 0 {
		result = f.results[0]
		f.results = f.results[1:]
	}
	return result, nil
}

type fakeEnricher struct{ starts int }

func (f *fakeEnricher) Available() bool { return true }
func (f *fakeEnricher) Start(after func(EnrichmentResult, error)) error {
	f.starts++
	after(EnrichmentResult{ImagePaths: []string{"/media/movie"}}, nil)
	return nil
}

type fakeLibrary struct {
	refreshes int
	images    [][]string
}

func (f *fakeLibrary) Refresh(context.Context) error {
	f.refreshes++
	return nil
}

func (f *fakeLibrary) RefreshImages(_ context.Context, paths []string) error {
	f.images = append(f.images, paths)
	return nil
}

func TestImportedResourcesFlowThroughReplaceableModules(t *testing.T) {
	publisher := &fakePublisher{results: []PublishResult{{Written: 1}, {Written: 1}}}
	enricher := &fakeEnricher{}
	library := &fakeLibrary{}
	bus := eventbus.New()
	_ = New(publisher, enricher, library, bus)

	if err := bus.Publish(context.Background(), mediaevents.ResourceImported{Trigger: "import", Added: 1}); err != nil {
		t.Fatal(err)
	}

	if len(publisher.calls) != 2 || publisher.calls[0] != "import" || publisher.calls[1] != "metadata classification" {
		t.Fatalf("unexpected publication calls: %#v", publisher.calls)
	}
	if enricher.starts != 1 {
		t.Fatalf("expected one enrichment, got %d", enricher.starts)
	}
	if len(library.images) != 1 || len(library.images[0]) != 1 {
		t.Fatalf("expected image refresh, got %#v", library.images)
	}
	if library.refreshes != 2 {
		t.Fatalf("expected one library refresh per changed publication, got %d", library.refreshes)
	}
}
