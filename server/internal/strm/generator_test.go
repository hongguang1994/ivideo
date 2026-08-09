package strm

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"ivideo/server/internal/store"
)

func TestPlanLayoutAlwaysWritesLockedFallbackNFO(t *testing.T) {
	g := &Generator{mediaDir: t.TempDir()}
	resource := store.Resource{ID: 42, Title: "04涂涂的徒弟", FilePath: "/熊出没/04涂涂的徒弟.mp4"}
	info := MediaInfo{Title: "熊出没之怪兽计划", Kind: KindEpisode, Season: 1, Episode: 4, OverrideLibrary: LibAnime}

	lo := g.planLayout(resource, info)
	if lo.nfoRel != filepath.Join("anime", "熊出没之怪兽计划", "tvshow.nfo") {
		t.Fatalf("NFO 路径错误: %s", lo.nfoRel)
	}
	for _, want := range []string{"<title>熊出没之怪兽计划</title>", "<lockdata>true</lockdata>"} {
		if !strings.Contains(lo.nfoContent, want) {
			t.Fatalf("兜底 NFO 缺少 %q:\n%s", want, lo.nfoContent)
		}
	}
}

func TestReviewLayoutIsIsolatedAndLocked(t *testing.T) {
	g := &Generator{mediaDir: t.TempDir()}
	resource := store.Resource{ID: 7, Title: "02", FilePath: "/未知动画/02.mp4"}
	info := ParsePath(resource.FilePath, resource.Title)
	info.OverrideLibrary = LibReview

	lo := g.planLayout(resource, info)
	if !strings.HasPrefix(lo.strmRel, filepath.Join("review", "resource-7 - ")) {
		t.Fatalf("待整理资源未隔离: %s", lo.strmRel)
	}
	if !strings.Contains(lo.nfoContent, "<lockdata>true</lockdata>") {
		t.Fatalf("待整理 NFO 未锁定: %s", lo.nfoContent)
	}
}

func TestRemoveOrphanShowDirs(t *testing.T) {
	mediaDir := t.TempDir()
	orphan := filepath.Join(mediaDir, "anime", "番外")
	orphanMovie := filepath.Join(mediaDir, "movies", "错误电影")
	active := filepath.Join(mediaDir, "anime", "勇者大冒险", "Season 01")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "tvshow.nfo"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(orphanMovie, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphanMovie, "错误电影.nfo"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, "勇者大冒险 S01E01.strm"), []byte("url"), 0o644); err != nil {
		t.Fatal(err)
	}

	(&Generator{mediaDir: mediaDir}).removeOrphanShowDirs()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("孤儿作品目录未删除: %v", err)
	}
	if _, err := os.Stat(orphanMovie); !os.IsNotExist(err) {
		t.Fatalf("孤儿电影目录未删除: %v", err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("仍含 STRM 的目录不应删除: %v", err)
	}
}

func TestGeneratorRequiresVerifiedMediaGroup(t *testing.T) {
	st, err := store.Open("sqlite", filepath.Join(t.TempDir(), "ivideo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	resourceID, err := st.AddResource(store.Resource{Title: "01", Provider: "aliyun", ShareURL: "https://pan.example.com/s/demo", FilePath: "/动漫/正确动画/01.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := st.SyncMediaGroup(store.MediaGroup{GroupKey: "demo", RawTitle: "正确动画", MediaKind: "episode", SuggestedLibrary: "anime", Status: "review"}, []store.MediaGroupMember{{ResourceID: resourceID, Season: 1, Episode: 1}})
	if err != nil {
		t.Fatal(err)
	}
	mediaDir := t.TempDir()
	g := New(st, mediaDir, "http://ivideo", "original", "/api/v1")
	if _, err := g.Generate(); err != nil {
		t.Fatal(err)
	}
	if matches, _ := filepath.Glob(filepath.Join(mediaDir, "review", "*", "*.strm")); len(matches) != 1 {
		var files []string
		_ = filepath.Walk(mediaDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && info != nil && !info.IsDir() {
				files = append(files, path)
			}
			return nil
		})
		t.Fatalf("未验证资源必须在待整理: matches=%v files=%v", matches, files)
	}
	if err := st.SetMediaGroupDecision(groupID, store.MediaGroupDecision{Status: "verified", DecisionSource: "manual", SelectedSource: "tmdb", SelectedID: "42", CanonicalTitle: "正确动画", Library: "anime", Confidence: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Generate(); err != nil {
		t.Fatal(err)
	}
	formal := filepath.Join(mediaDir, "anime", "正确动画", "Season 01", "正确动画 S01E01.strm")
	if _, err := os.Stat(formal); err != nil {
		t.Fatalf("已验证作品未发布: %v", err)
	}
	detail, err := st.GetMediaGroupDetail(groupID)
	if err != nil || detail.Group.Status != "published" {
		t.Fatalf("未记录发布状态: %+v err=%v", detail.Group, err)
	}
}

func TestGeneratorDeduplicatesSameVerifiedWorkAndIsIdempotent(t *testing.T) {
	st, err := store.Open("sqlite", filepath.Join(t.TempDir(), "ivideo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	firstID := addVerifiedMovie(t, st, "first", "同一电影", "/来源一/同一电影.2024.mkv", "9001")
	secondID := addVerifiedMovie(t, st, "second", "同一电影", "/来源二/同一电影.2024.mp4", "9001")
	mediaDir := t.TempDir()
	g := New(st, mediaDir, "http://ivideo", "original", "/api/v1")

	first, err := g.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if first.Written != 1 || first.Deduplicated != 1 || first.Conflicts != 0 {
		t.Fatalf("首次发布统计错误: %+v", first)
	}
	files := findSTRMFiles(t, mediaDir)
	if len(files) != 1 {
		t.Fatalf("同一作品应只生成一个 STRM: %v", files)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	winner := firstID
	if secondID < winner {
		winner = secondID
	}
	if !strings.Contains(string(content), "/file/"+fmt.Sprint(winner)) {
		t.Fatalf("重复来源未稳定选择最小资源 ID: %s", content)
	}

	second, err := g.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if second.Written != 0 || second.Removed != 0 || second.Unchanged != 2 {
		t.Fatalf("连续生成不幂等: %+v", second)
	}
}

func TestGeneratorIsolatesDifferentWorksWithSameTitle(t *testing.T) {
	st, err := store.Open("sqlite", filepath.Join(t.TempDir(), "ivideo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	addVerifiedMovie(t, st, "work-a", "重名电影", "/A/重名电影.2020.mkv", "100")
	addVerifiedMovie(t, st, "work-b", "重名电影", "/B/重名电影.2020.mkv", "200")
	mediaDir := t.TempDir()
	g := New(st, mediaDir, "http://ivideo", "original", "/api/v1")

	first, err := g.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if first.Written != 2 || first.Conflicts != 2 || first.Deduplicated != 0 {
		t.Fatalf("同名冲突统计错误: %+v", first)
	}
	files := findSTRMFiles(t, mediaDir)
	if len(files) != 2 || files[0] == files[1] {
		t.Fatalf("不同作品未隔离: %v", files)
	}
	for _, path := range files {
		if !strings.Contains(path, "ivideo-group-") {
			t.Fatalf("冲突目录缺少稳定作品标识: %s", path)
		}
	}

	second, err := g.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if second.Written != 0 || second.Removed != 0 {
		t.Fatalf("冲突隔离后连续生成不幂等: %+v", second)
	}
}

func addVerifiedMovie(t *testing.T, st store.Store, key, title, path, providerID string) int64 {
	t.Helper()
	resourceID, err := st.AddResource(store.Resource{
		Title: title, Provider: "aliyun", ShareURL: "https://pan.example.com/s/" + key, FilePath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := st.SyncMediaGroup(store.MediaGroup{
		GroupKey: key, RawTitle: title, NormalizedTitle: title, MediaKind: "movie",
		SuggestedLibrary: "movies", Status: "verified",
	}, []store.MediaGroupMember{{ResourceID: resourceID, Confidence: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMediaGroupDecision(groupID, store.MediaGroupDecision{
		Status: "verified", DecisionSource: "manual", SelectedSource: "tmdb", SelectedID: providerID,
		CanonicalTitle: title, Library: "movies", Confidence: 100,
	}); err != nil {
		t.Fatal(err)
	}
	return resourceID
}

func findSTRMFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".strm") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}
