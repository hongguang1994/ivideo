package metadata

import (
	"path/filepath"
	"testing"

	"ivideo/server/internal/store"
	"ivideo/server/internal/strm"
)

func TestCanonicalItemPathUsesProviderTitle(t *testing.T) {
	root := t.TempDir()
	service := New(nil, root, "")
	group := &mediaGroup{
		info:      strm.MediaInfo{Title: "一日雄狮.2023.1080p", Kind: strm.KindMovie},
		resources: []store.Resource{{ID: 1}},
	}

	dir, base := service.canonicalItemPath(group, "一日雄狮", strm.LibMovies)
	if want := filepath.Join(root, "movies", "一日雄狮"); dir != want {
		t.Fatalf("canonical directory = %q, want %q", dir, want)
	}
	if base != "一日雄狮" || group.info.Title != "一日雄狮" {
		t.Fatalf("canonical title was not retained: base=%q info=%q", base, group.info.Title)
	}
}
