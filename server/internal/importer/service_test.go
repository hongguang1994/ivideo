package importer

import (
	"context"
	"path"
	"strings"
	"testing"

	"ivideo/server/internal/eventbus"
	"ivideo/server/internal/mediaevents"
)

type fakeSource struct {
	walk      []Entry
	supported bool
	listed    map[string][]Entry
}

func (f fakeSource) Walk(context.Context, ShareRef) ([]Entry, bool, error) {
	return f.walk, f.supported, nil
}

func (f fakeSource) List(_ context.Context, _ ShareRef, subPath string) ([]Entry, error) {
	return f.listed[subPath], nil
}

type fakeCatalog struct {
	existing map[string]struct{}
	added    []Resource
}

func (f *fakeCatalog) ExistingKeys(context.Context) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(f.existing))
	for key := range f.existing {
		result[key] = struct{}{}
	}
	return result, nil
}

func (f *fakeCatalog) Add(_ context.Context, resource Resource) (bool, error) {
	f.added = append(f.added, resource)
	return true, nil
}

func videoMatcher(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".mkv", ".mp4":
		return true
	default:
		return false
	}
}

func TestServiceImportsThroughPortsAndSkipsDuplicates(t *testing.T) {
	catalog := &fakeCatalog{existing: map[string]struct{}{
		resourceKey("https://share/one", "/movie-a.mkv"): {},
	}}
	service := New(fakeSource{supported: true, walk: []Entry{
		{Name: "movie-a.mkv", Path: "/movie-a.mkv"},
		{Name: "movie-b.mp4", Path: "/movie-b.mp4"},
		{Name: "readme.txt", Path: "/readme.txt"},
	}}, catalog, VideoMatcherFunc(videoMatcher), Options{MaxDepth: 3, MaxFiles: 10})

	result := service.Import(context.Background(), ShareRef{Provider: "aliyun", URL: "https://share/one"})
	if result.Added != 1 || result.Skipped != 1 || len(result.Errors) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(catalog.added) != 1 || catalog.added[0].Title != "movie-b" {
		t.Fatalf("unexpected resources: %#v", catalog.added)
	}
}

func TestServiceFallsBackToBoundedDirectoryTraversal(t *testing.T) {
	catalog := &fakeCatalog{existing: map[string]struct{}{}}
	service := New(fakeSource{listed: map[string][]Entry{
		"":        {{Name: "season", Path: "/season", IsDir: true}},
		"/season": {{Name: "episode-01.mkv", Path: "/season/episode-01.mkv"}, {Name: "deep", Path: "/season/deep", IsDir: true}},
	}}, catalog, VideoMatcherFunc(videoMatcher), Options{MaxDepth: 1, MaxFiles: 10})

	result := service.Import(context.Background(), ShareRef{Provider: "quark", URL: "https://share/two"})
	if result.Added != 1 || len(result.Errors) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if got := catalog.added[0].FilePath; got != "/season/episode-01.mkv" {
		t.Fatalf("unexpected path: %s", got)
	}
}

func TestServicePublishesResourceImportedEvent(t *testing.T) {
	bus := eventbus.New()
	var imported mediaevents.ResourceImported
	bus.Subscribe(mediaevents.ResourceImportedName, func(_ context.Context, event eventbus.Event) error {
		imported = event.(mediaevents.ResourceImported)
		return nil
	})
	catalog := &fakeCatalog{existing: map[string]struct{}{}}
	service := New(fakeSource{supported: true, walk: []Entry{{Name: "movie.mkv", Path: "/movie.mkv"}}},
		catalog, VideoMatcherFunc(videoMatcher), Options{MaxFiles: 10}, WithEvents(bus))

	result := service.Import(context.Background(), ShareRef{Provider: "aliyun", URL: "https://share/event", Trigger: "manual"})
	if result.Added != 1 || len(result.Errors) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if imported.Added != 1 || imported.Trigger != "manual" || imported.Provider != "aliyun" {
		t.Fatalf("unexpected event: %#v", imported)
	}
}
