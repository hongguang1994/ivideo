package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestExecSchemaIgnoresCommentSemicolons(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := execSchema(db, "-- 注释; 含分号\nCREATE TABLE sample (id INTEGER PRIMARY KEY);"); err != nil {
		t.Fatal(err)
	}
}

func TestShareSourceKeyNormalizesURL(t *testing.T) {
	first := shareSourceKey("AliYun", "HTTPS://Pan.Example.com/s/code#ignored")
	second := shareSourceKey("aliyun", "https://pan.example.com/s/code")
	if first != second {
		t.Fatalf("source keys differ: %s != %s", first, second)
	}
}

func TestResourceIncludesShareSourceIdentityContext(t *testing.T) {
	st, err := Open("sqlite", filepath.Join(t.TempDir(), "ivideo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	shareURL := "https://pan.example.com/s/identity"
	if _, err := st.AddShare(Share{Title: "师兄啊师兄", Category: "国漫", Provider: "quark", ShareURL: shareURL}); err != nil {
		t.Fatal(err)
	}
	resourceID, err := st.AddResource(Resource{Title: "146 4K", Provider: "quark", ShareURL: shareURL, FilePath: "/S🐻/146 4K.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := st.GetResource(resourceID)
	if err != nil {
		t.Fatal(err)
	}
	if resource.SourceTitle != "师兄啊师兄" || resource.SourceCategory != "国漫" {
		t.Fatalf("missing source context: %+v", resource)
	}
}

func TestDeleteShareKeepsImportedResource(t *testing.T) {
	st, err := Open("sqlite", filepath.Join(t.TempDir(), "ivideo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	shareID, err := st.AddShare(Share{Provider: "aliyun", ShareURL: "https://pan.example.com/s/code"})
	if err != nil {
		t.Fatal(err)
	}
	resourceID, err := st.AddResource(Resource{Title: "Example", Provider: "aliyun", ShareURL: "https://pan.example.com/s/code", FilePath: "/video.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteShare(shareID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetResource(resourceID); err != nil {
		t.Fatalf("resource was removed with its bookmark: %v", err)
	}
	shares, err := st.ListShares()
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 0 {
		t.Fatalf("shares after unbookmark = %d, want 0", len(shares))
	}
}

func TestSQLiteResourceDeduplicationAndCacheForeignKey(t *testing.T) {
	st, err := Open("sqlite", filepath.Join(t.TempDir(), "ivideo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	resource := Resource{
		Title: "Example", Provider: "aliyun", ShareURL: "https://pan.example.com/s/code", FilePath: "/video.mkv",
	}
	id, err := st.AddResource(resource)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTransferring(id, "aliyun"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddResource(resource); !errors.Is(err, ErrResourceExists) {
		t.Fatalf("duplicate error = %v, want ErrResourceExists", err)
	}
	if err := st.SetTransferring(id+1, "aliyun"); err == nil {
		t.Fatal("expected foreign key violation for an unknown resource")
	}
}

func TestMediaGroupLifecycle(t *testing.T) {
	st, err := Open("sqlite", filepath.Join(t.TempDir(), "ivideo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	one, err := st.AddResource(Resource{Title: "01", Provider: "aliyun", ShareURL: "https://pan.example.com/s/group", FilePath: "/动漫/示例/01.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := st.AddResource(Resource{Title: "02", Provider: "aliyun", ShareURL: "https://pan.example.com/s/group", FilePath: "/动漫/示例/02.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := st.SyncMediaGroup(MediaGroup{GroupKey: "group-key", RawTitle: "示例", NormalizedTitle: "示例", MediaKind: "episode", SuggestedLibrary: "anime"}, []MediaGroupMember{{ResourceID: one, Season: 1, Episode: 1}, {ResourceID: two, Season: 1, Episode: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceMediaCandidates(groupID, []MediaCandidate{{Source: "tmdb", ProviderID: "42", Title: "示例", Library: "anime", Score: 95, EvidenceJSON: `{"titleExact":true}`}}); err != nil {
		t.Fatal(err)
	}
	detail, err := st.GetMediaGroupDetail(groupID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Members) != 2 || len(detail.Candidates) != 1 {
		t.Fatalf("作品组内容错误: %+v", detail)
	}
	if err := st.SetMediaGroupDecision(groupID, MediaGroupDecision{Status: "verified", DecisionSource: "manual", SelectedSource: "tmdb", SelectedID: "42", CanonicalTitle: "示例", Library: "anime", Confidence: 100}); err != nil {
		t.Fatal(err)
	}
	states, err := st.GetResourceGroupStates()
	if err != nil || states[one].Status != "verified" || states[two].GroupID != groupID {
		t.Fatalf("成员状态错误: %#v err=%v", states, err)
	}
	if err := st.RecordMediaPublication(MediaPublication{GroupID: groupID, OutputPath: "anime/示例", MetadataHash: "hash"}); err != nil {
		t.Fatal(err)
	}
	detail, err = st.GetMediaGroupDetail(groupID)
	if err != nil || detail.Group.Status != "published" {
		t.Fatalf("发布状态错误: %+v err=%v", detail.Group, err)
	}
}

func TestMediaAliasLifecycle(t *testing.T) {
	st, err := Open("sqlite", filepath.Join(t.TempDir(), "ivideo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	alias := MediaAlias{Alias: "One Hundred Thousand Bad Jokes", CanonicalTitle: "十万个冷笑话",
		MediaKind: "tv", Source: "tmdb", ProviderID: "42", Year: 2012, Confidence: 100}
	if err := st.UpsertMediaAlias(alias); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertMediaAlias(alias); err != nil {
		t.Fatal(err)
	}
	items, err := st.FindMediaAliases("one-hundred_thousand bad jokes", "tv", 2012)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].CanonicalTitle != "十万个冷笑话" || items[0].HitCount != 2 {
		t.Fatalf("unexpected aliases: %+v", items)
	}
	if items, err := st.FindMediaAliases(alias.Alias, "movie", 2012); err != nil || len(items) != 0 {
		t.Fatalf("media kind conflict should not match: %+v err=%v", items, err)
	}
}

func TestDeleteOrphanMediaGroupsAfterMemberMoves(t *testing.T) {
	st, err := Open("sqlite", filepath.Join(t.TempDir(), "ivideo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	resourceID, err := st.AddResource(Resource{Title: "01", Provider: "aliyun", ShareURL: "https://pan.example.com/s/move", FilePath: "/动漫/作品/01.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	oldID, err := st.SyncMediaGroup(MediaGroup{GroupKey: "old", RawTitle: "旧解析"}, []MediaGroupMember{{ResourceID: resourceID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SyncMediaGroup(MediaGroup{GroupKey: "new", RawTitle: "新解析"}, []MediaGroupMember{{ResourceID: resourceID}}); err != nil {
		t.Fatal(err)
	}
	removed, err := st.DeleteOrphanMediaGroups()
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err := st.GetMediaGroupDetail(oldID); err == nil {
		t.Fatal("obsolete empty group still exists")
	}
}

func TestMediaEvidenceAndPublicationLifecycle(t *testing.T) {
	st, err := Open("sqlite", filepath.Join(t.TempDir(), "ivideo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	resourceID, err := st.AddResource(Resource{Title: "测试电影", Provider: "aliyun", ShareURL: "https://pan.example.com/s/evidence", FilePath: "/电影/测试电影.2024.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveMediaPathAnalysis(MediaPathAnalysis{ResourceID: resourceID, AnalyzerVersion: "semantic-v1", PathHash: "hash", AnalysisJSON: `{"specificTitle":"测试电影"}`}, []MediaTitleCandidate{{Title: "测试电影", Year: 2024, Source: "文件名", Weight: 15, AutoEligible: true}}); err != nil {
		t.Fatal(err)
	}
	analysis, pathCandidates, found, err := st.GetMediaPathAnalysis(resourceID)
	if err != nil || !found || analysis.AnalyzerVersion != "semantic-v1" || len(pathCandidates) != 1 || !pathCandidates[0].AutoEligible {
		t.Fatalf("路径证据错误: analysis=%+v candidates=%+v found=%v err=%v", analysis, pathCandidates, found, err)
	}
	groupID, err := st.SyncMediaGroup(MediaGroup{GroupKey: "evidence-group", RawTitle: "测试电影", MediaKind: "movie"}, []MediaGroupMember{{ResourceID: resourceID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordMediaVerification(MediaVerification{GroupID: groupID, ResourceID: resourceID, Verifier: "metadata_match", VerifierVersion: "strict-v1", Status: "supporting", ScoreDelta: 95, Reason: "标题一致"}); err != nil {
		t.Fatal(err)
	}
	verifications, err := st.ListMediaVerifications(groupID)
	if err != nil || len(verifications) != 1 || verifications[0].ScoreDelta != 95 {
		t.Fatalf("复核记录错误: %+v err=%v", verifications, err)
	}
	jobID, err := st.StartMediaProcessingJob(MediaProcessingJob{Kind: "metadata", TotalCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordMediaProcessingStep(MediaProcessingStep{JobID: jobID, GroupID: groupID, ResourceID: resourceID, Stage: "metadata_match", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateMediaProcessingJob(MediaProcessingJob{ID: jobID, Kind: "metadata", Status: "completed", TotalCount: 1, ProcessedCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordMediaPublicationArtifact(MediaPublicationArtifact{GroupID: groupID, ArtifactType: "nfo", Path: "/media/movies/测试电影/测试电影.nfo", ContentHash: "hash"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceMediaGroupTags(groupID, []MediaTag{{Name: "动作", Source: "tmdb", Confidence: 95}, {Name: "中国大陆", Source: "path", Confidence: 80}}); err != nil {
		t.Fatal(err)
	}
	tags, err := st.ListMediaGroupTags(groupID)
	if err != nil || len(tags) != 2 {
		t.Fatalf("标签记录错误: %+v err=%v", tags, err)
	}
}
