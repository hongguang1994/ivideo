package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenMigratesManualLegacyMatchAndDropsOldTables(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	resourceID, err := s.AddResource(Resource{
		Title: "测试电影", Provider: "aliyun", ShareURL: "https://example.test/s/1", FilePath: "/测试电影.mp4",
	})
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := s.SyncMediaGroup(MediaGroup{
		GroupKey: "legacy-test", RawTitle: "测试电影", NormalizedTitle: "测试电影", SuggestedLibrary: "review",
	}, []MediaGroupMember{{ResourceID: resourceID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	legacySchema := `
		CREATE TABLE media_matches (resource_id INTEGER PRIMARY KEY, source TEXT NOT NULL, provider_id TEXT NOT NULL,
			title TEXT NOT NULL, year INTEGER NOT NULL, confidence INTEGER NOT NULL, status TEXT NOT NULL,
			reason TEXT NOT NULL, updated_at INTEGER NOT NULL);
		CREATE TABLE media_classifications (resource_id INTEGER PRIMARY KEY, library TEXT NOT NULL, source TEXT NOT NULL,
			updated_at INTEGER NOT NULL);
		CREATE TABLE import_jobs (id INTEGER PRIMARY KEY AUTOINCREMENT);
	`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO media_matches VALUES (?, 'tmdb', '1234', '正确片名', 2024, 99, 'confirmed', '人工确认', ?)`, resourceID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO media_classifications VALUES (?, 'movies', 'manual', ?)`, resourceID, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	detail, err := s.GetMediaGroupDetail(groupID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Group.DecisionSource != "manual" || detail.Group.SelectedID != "1234" || detail.Group.CanonicalTitle != "正确片名" {
		t.Fatalf("legacy decision was not preserved: %+v", detail.Group)
	}
	if detail.Group.SuggestedLibrary != "movies" || detail.Group.Status != "verified" {
		t.Fatalf("legacy classification was not preserved: %+v", detail.Group)
	}

	raw := s.(*sqlStore).db
	for _, table := range []string{"media_matches", "media_classifications", "import_jobs"} {
		exists, err := legacyTableExists(raw, sqliteDialect, table)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("legacy table %s still exists", table)
		}
	}
}
