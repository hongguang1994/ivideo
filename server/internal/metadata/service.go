package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ivideo/server/internal/store"
	"ivideo/server/internal/strm"
)

var ErrBusy = errors.New("刮削任务正在运行")
var ErrNeedsReview = errors.New("匹配结果需要人工确认")

type Result struct {
	Items      int      `json:"items"`
	Episodes   int      `json:"episodes"`
	Images     int      `json:"images"`
	Douban     int      `json:"douban"`
	Skipped    int      `json:"skipped"`
	Errors     []string `json:"errors,omitempty"`
	ImagePaths []string `json:"-"`
}
type Status struct {
	Configured bool    `json:"configured"`
	Running    bool    `json:"running"`
	StartedAt  int64   `json:"startedAt,omitempty"`
	FinishedAt int64   `json:"finishedAt,omitempty"`
	LastResult *Result `json:"lastResult,omitempty"`
	LastError  string  `json:"lastError,omitempty"`
}
type Service struct {
	store              Repository
	mediaDir           string
	siteURL            string
	pathAnalyzer       PathAnalyzer
	candidateGenerator CandidateGenerator
	matchEngine        MatchEngine
	contentVerifier    ContentVerifier
	publisher          MediaPublisher
	running            atomic.Bool
	contentBudget      atomic.Int32
	mu                 sync.RWMutex
	last               Status
}

// Repository composes only the persistence domains needed by metadata.
type Repository interface {
	ListResources() ([]store.Resource, error)
	store.CredentialRepository
	store.MediaRepository
	store.JobRepository
}

func New(st Repository, mediaDir, siteURL string, options ...Option) *Service {
	s := &Service{
		store: st, mediaDir: mediaDir, siteURL: strings.TrimRight(siteURL, "/"),
		pathAnalyzer: semanticPathAnalyzer{}, candidateGenerator: semanticCandidateGenerator{},
		matchEngine: strictMatchEngine{}, publisher: localMediaPublisher{},
	}
	s.contentVerifier = serviceContentVerifier{service: s}
	for _, option := range options {
		option(s)
	}
	return s
}

func (s *Service) Status() (Status, error) {
	_, ok, err := s.store.GetCredential("tmdb")
	s.mu.RLock()
	status := s.last
	s.mu.RUnlock()
	status.Configured = ok
	status.Running = s.running.Load()
	return status, err
}

func (s *Service) SaveToken(ctx context.Context, token string) error {
	if token == "" {
		return errors.New("TMDb Read Access Token 不能为空")
	}
	if err := newTMDb(token).verify(ctx); err != nil {
		return fmt.Errorf("Token 校验失败: %w", err)
	}
	return s.store.SetCredential("tmdb", token, "read_access_token")
}

// PreferredDiscoveryQuery 将副标题、简称或其他译名转换为更容易被资源索引收录的片名。
// 无 Token、TMDb 未命中或查询失败时返回原词，不能影响主搜索链路。
func (s *Service) PreferredDiscoveryQuery(ctx context.Context, query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return query
	}
	cred, ok, err := s.store.GetCredential("tmdb")
	if err != nil || !ok || strings.TrimSpace(cred.Token) == "" {
		return query
	}
	client := newTMDb(cred.Token)
	var fallback string
	for _, kind := range []string{"movie", "tv"} {
		items, searchErr := client.searchResults(ctx, kind, query, 0)
		if searchErr != nil || len(items) == 0 {
			continue
		}
		if fallback == "" {
			fallback = discoveryTitleStem(firstTitle(items[0]))
		}
		if preferred := preferredDiscoveryTerm(query, items); preferred != "" {
			return preferred
		}
	}
	if fallback != "" {
		return fallback
	}
	return query
}

func preferredDiscoveryTerm(query string, items []tmdbItem) string {
	want := normalizeTitle(query)
	for index, item := range items {
		if index >= 5 {
			break
		}
		for _, candidate := range []string{firstTitle(item), firstOriginalTitle(item)} {
			key := normalizeTitle(candidate)
			if key != "" && want != "" && (strings.Contains(key, want) || strings.Contains(want, key)) {
				return discoveryTitleStem(firstTitle(item))
			}
		}
	}
	return ""
}

func discoveryTitleStem(title string) string {
	title = strings.TrimSpace(title)
	for _, separator := range []string{"：", ":", " - ", " – ", " — "} {
		if index := strings.Index(title, separator); index > 0 {
			prefix := strings.TrimSpace(title[:index])
			if len([]rune(prefix)) >= 2 {
				return prefix
			}
		}
	}
	return title
}

// SearchCandidates 为人工整理提供作品级候选，不会直接发布任何结果。
func (s *Service) SearchCandidates(ctx context.Context, groupID int64, query string) (store.MediaGroupDetail, error) {
	detail, err := s.store.GetMediaGroupDetail(groupID)
	if err != nil {
		return detail, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		query = detail.Group.RawTitle
	}
	cred, ok, err := s.store.GetCredential("tmdb")
	if err != nil || !ok || cred.Token == "" {
		return detail, errors.New("请先配置 TMDb Read Access Token")
	}
	kind := "tv"
	if detail.Group.MediaKind == string(strm.KindMovie) {
		kind = "movie"
	}
	provider := tmdbProvider{client: newTMDb(cred.Token)}
	results, err := provider.Search(ctx, MetadataQuery{Kind: kind, Title: query, Year: detail.Group.Year, RequireAnimation: detail.Group.SuggestedLibrary == string(strm.LibAnime)})
	if err != nil {
		return detail, err
	}
	candidates := make([]store.MediaCandidate, 0, min(len(results), 10))
	requireAnimation := detail.Group.SuggestedLibrary == string(strm.LibAnime)
	for _, item := range results {
		if len(candidates) >= 10 {
			break
		}
		decision := s.matchEngine.Evaluate(MatchInput{
			Query:     MetadataQuery{Kind: kind, Title: query, Year: detail.Group.Year, RequireAnimation: requireAnimation},
			Candidate: item, PathWeight: 12, PathAutoEligible: true,
		})
		library := strm.LibTV
		typeMatch := true
		if kind == "movie" {
			library = strm.LibMovies
		} else if item.Animation {
			library = strm.LibAnime
		}
		candidates = append(candidates, store.MediaCandidate{
			GroupID: groupID, Source: provider.Name(), ProviderID: item.ProviderID, Title: item.Title,
			OriginalTitle: item.OriginalTitle, Year: item.Year, MediaKind: kind,
			Library: string(library), Score: decision.Score,
			EvidenceJSON: evidenceJSON(decision.Reason, normalizeTitle(query) == normalizeTitle(item.Title) || normalizeTitle(query) == normalizeTitle(item.OriginalTitle), detail.Group.Year > 0 && item.Year == detail.Group.Year, typeMatch),
		})
	}
	if err := s.store.ReplaceMediaCandidates(groupID, candidates); err != nil {
		return detail, err
	}
	return s.store.GetMediaGroupDetail(groupID)
}

type mediaGroup struct {
	id        int64
	info      strm.MediaInfo
	resources []store.Resource
}

func (s *Service) Scrape(ctx context.Context) (Result, error) {
	var result Result
	if !s.running.CompareAndSwap(false, true) {
		return result, ErrBusy
	}
	s.markStarted()
	result, err := s.run(ctx)
	s.finish(result, err)
	return result, err
}

// Start 在后台启动刮削，避免大媒体库被反向代理的请求超时中断。
func (s *Service) Start(after func(Result, error)) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrBusy
	}
	s.markStarted()
	go func() {
		result, err := s.run(context.Background())
		s.finish(result, err)
		if after != nil {
			after(result, err)
		}
	}()
	return nil
}

func (s *Service) markStarted() {
	s.mu.Lock()
	s.last = Status{Running: true, StartedAt: time.Now().Unix()}
	s.mu.Unlock()
}

func (s *Service) finish(result Result, err error) {
	s.mu.Lock()
	s.last.Running = false
	s.last.FinishedAt = time.Now().Unix()
	s.last.LastResult = &result
	if err != nil {
		s.last.LastError = err.Error()
	}
	s.mu.Unlock()
	s.running.Store(false)
}

func (s *Service) run(ctx context.Context) (result Result, runErr error) {
	// 内容分析涉及远程取样，限制单轮数量以免占满代理带宽；缓存命中不重复取样。
	s.contentBudget.Store(24)
	cred, ok, err := s.store.GetCredential("tmdb")
	if err != nil {
		return result, err
	}
	if !ok || cred.Token == "" {
		return result, errors.New("请先配置 TMDb Read Access Token")
	}
	resources, err := s.store.ListResources()
	if err != nil {
		return result, err
	}
	job := store.MediaProcessingJob{Kind: "metadata", Status: "running", TotalCount: len(resources)}
	job.ID, err = s.store.StartMediaProcessingJob(job)
	if err != nil {
		return result, fmt.Errorf("创建媒体整理任务: %w", err)
	}
	defer func() {
		job.Status = "completed"
		if runErr != nil {
			job.Status, job.Error = "failed", runErr.Error()
		}
		_ = s.store.UpdateMediaProcessingJob(job)
	}()
	states, err := s.store.GetResourceGroupStates()
	if err != nil {
		return result, err
	}
	groups := map[string]*mediaGroup{}
	analyses := make(map[int64]strm.PathAnalysis, len(resources))
	for _, r := range resources {
		analysis := s.analyzePath(r)
		analyses[r.ID] = analysis
		if err := s.persistPathEvidence(r, analysis); err != nil {
			return result, fmt.Errorf("保存路径分析 %d: %w", r.ID, err)
		}
		_ = s.store.RecordMediaProcessingStep(store.MediaProcessingStep{
			JobID: job.ID, ResourceID: r.ID, Stage: "path_analysis", Status: "completed",
		})
		info := analysis.Info
		// 已经由刮削或人工确认的分类优先于分享路径中的分类词。
		// 否则来源目录里带“动漫”的真人资源会在每次刮削时重新被当成动画。
		if state, ok := states[r.ID]; ok && (state.Status == "verified" || state.Status == "published") && strm.ValidLibrary(state.Library) {
			info.OverrideLibrary = strm.LibraryKind(state.Library)
		}
		key := string(info.Library()) + "\x00" + info.Title
		if info.Kind == strm.KindMovie {
			key += "\x00" + strconv.FormatInt(r.ID, 10)
		}
		if groups[key] == nil {
			groups[key] = &mediaGroup{info: info}
		}
		groups[key].resources = append(groups[key].resources, r)
	}
	for key, group := range groups {
		digest := sha256.Sum256([]byte(key))
		members := make([]store.MediaGroupMember, 0, len(group.resources))
		for _, resource := range group.resources {
			memberInfo := analyses[resource.ID].Info
			confidence := 60
			if memberInfo.Kind == strm.KindEpisode && memberInfo.Episode > 0 {
				confidence = 90
			}
			members = append(members, store.MediaGroupMember{ResourceID: resource.ID, Season: memberInfo.Season, Episode: memberInfo.Episode, Confidence: confidence})
		}
		groupID, err := s.store.SyncMediaGroup(store.MediaGroup{
			GroupKey: hex.EncodeToString(digest[:]), RawTitle: group.info.Title,
			NormalizedTitle: normalizeTitle(group.info.Title), MediaKind: string(group.info.Kind),
			SuggestedLibrary: string(group.info.Library()), Year: group.info.Year,
			Status: "grouped", DecisionSource: "auto",
		}, members)
		if err != nil {
			return result, fmt.Errorf("保存作品组 %s: %w", group.info.Title, err)
		}
		group.id = groupID
	}
	if _, err := s.store.DeleteOrphanMediaGroups(); err != nil {
		return result, fmt.Errorf("清理旧作品组: %w", err)
	}
	job.TotalCount = len(groups)
	_ = s.store.UpdateMediaProcessingJob(job)
	client := newTMDb(cred.Token)
	douban := newDouban()
	for _, group := range groups {
		if err := s.scrapeGroup(ctx, client, group, &result); err != nil {
			// TMDb 未命中或置信度不足时都使用豆瓣交叉验证。豆瓣搜索
			// 仍要求标题完全一致、年份不冲突，因此不会放宽错误匹配。
			if doubanErr := s.scrapeGroupDouban(ctx, douban, group, &result); doubanErr == nil {
				job.ProcessedCount++
				_ = s.store.RecordMediaProcessingStep(store.MediaProcessingStep{JobID: job.ID, GroupID: group.id, ResourceID: group.resources[0].ID, Stage: "metadata_match", Status: "completed"})
				_ = s.store.UpdateMediaProcessingJob(job)
				continue
			} else if len(result.Errors) < 20 {
				result.Errors = append(result.Errors, group.info.Title+"（豆瓣兜底）: "+doubanErr.Error())
			}
			result.Skipped++
			job.ProcessedCount++
			job.FailedCount++
			_ = s.store.RecordMediaProcessingStep(store.MediaProcessingStep{JobID: job.ID, GroupID: group.id, ResourceID: group.resources[0].ID, Stage: "metadata_match", Status: "review", Error: err.Error()})
			_ = s.store.UpdateMediaProcessingJob(job)
			if len(result.Errors) < 20 {
				result.Errors = append(result.Errors, group.info.Title+": "+err.Error())
			}
			continue
		}
		job.ProcessedCount++
		_ = s.store.RecordMediaProcessingStep(store.MediaProcessingStep{JobID: job.ID, GroupID: group.id, ResourceID: group.resources[0].ID, Stage: "metadata_match", Status: "completed"})
		_ = s.store.UpdateMediaProcessingJob(job)
	}
	return result, nil
}

// scrapeGroupDouban 使用豆瓣作品页补齐 TMDb 未命中的作品。
// 豆瓣通常没有可靠的分集资料，因此分集只写入本地文件名和季集号，避免错误匹配剧照。
func (s *Service) scrapeGroupDouban(ctx context.Context, c *doubanClient, g *mediaGroup, result *Result) error {
	analysis := s.analyzePath(g.resources[0])
	queries := s.titleCandidates(g.resources[0], analysis)
	if len(queries) == 0 {
		return fmt.Errorf("没有可用于豆瓣搜索的可靠片名")
	}
	var found doubanItem
	var err error
	provider := doubanProvider{client: c}
	queryTitle := queries[0].Title
	for i, query := range queries {
		if i >= 6 {
			break
		}
		var candidates []MetadataCandidate
		candidates, err = provider.Search(ctx, MetadataQuery{Kind: string(g.info.Kind), Title: query.Title, Year: firstNonZero(query.Year, g.info.Year), RequireAnimation: requiresAnimation(g.info)})
		if err == nil && len(candidates) > 0 {
			found, _ = candidates[0].native.(doubanItem)
			queryTitle = query.Title
			break
		}
	}
	if err != nil {
		return err
	}
	library := g.info.Library()
	if library == strm.LibReview {
		original := s.analyzePath(g.resources[0]).Info
		library = original.Library()
	}
	name := found.Title
	if name == "" {
		name = queryTitle
	}
	itemDir, base := s.canonicalItemPath(g, name, library)
	tags := []string{}
	if country := g.info.Country(); country != "" {
		tags = append(tags, country)
	}
	ids := []uniqueID{{Type: "douban", Default: true, Value: found.ID}}
	s.persistCandidate(g, store.MediaCandidate{
		GroupID: g.id, Source: "douban", ProviderID: found.ID, Title: name,
		OriginalTitle: found.OriginalTitle, Year: found.Year, MediaKind: string(g.info.Kind),
		Library: string(library), Score: 90, EvidenceJSON: evidenceJSON("豆瓣标题完全一致", true, found.Year > 0, true),
	})
	s.setAutomaticDecision(g.id, store.MediaGroupDecision{
		Status: "verified", DecisionSource: "auto", SelectedSource: "douban", SelectedID: found.ID,
		CanonicalTitle: name, Library: string(library), Confidence: 90, Reason: "豆瓣标题一致且年份不冲突",
	})
	if g.info.Kind == strm.KindMovie {
		nfoPath := filepath.Join(itemDir, base+".nfo")
		if err := s.publisher.WriteNFO(nfoPath, movieNFO{
			Title: name, OriginalTitle: found.OriginalTitle, Plot: found.Plot,
			Year: found.Year, Rating: found.Rating, UniqueIDs: ids, Tags: tags, LockData: true,
		}); err != nil {
			return err
		}
		s.recordArtifact(g.id, "nfo", nfoPath, "ready", "")
	} else {
		nfoPath := filepath.Join(itemDir, "tvshow.nfo")
		if err := s.publisher.WriteNFO(nfoPath, tvshowNFO{
			Title: name, OriginalTitle: found.OriginalTitle, Plot: found.Plot,
			Year: found.Year, Rating: found.Rating, UniqueIDs: ids, Tags: tags, LockData: true,
		}); err != nil {
			return err
		}
		s.recordArtifact(g.id, "nfo", nfoPath, "ready", "")
		for _, r := range g.resources {
			info := s.analyzePath(r).Info
			epBase := fmt.Sprintf("%s S%02dE%02d", strm.Sanitize(g.info.Title), info.Season, info.Episode)
			epPath := filepath.Join(itemDir, fmt.Sprintf("Season %02d", info.Season), epBase+".nfo")
			if err := s.publisher.WriteNFO(epPath, episodeNFO{
				Title: r.Title, Season: info.Season, Episode: info.Episode, LockData: true,
			}); err != nil {
				return err
			}
			s.recordArtifact(g.id, "episode_nfo", epPath, "ready", "")
			result.Episodes++
		}
	}
	result.Items++
	result.Douban++
	imagesBefore := result.Images
	if found.Poster != "" {
		posterPath := filepath.Join(itemDir, "poster.jpg")
		if err := s.publisher.PublishImage(ctx, c, found.Poster, posterPath); err == nil {
			result.Images++
			s.recordArtifact(g.id, "poster", posterPath, "ready", "")
		} else {
			slog.Warn("豆瓣海报下载失败", "title", name, "url", found.Poster, "error", err)
		}
	}
	s.replaceGroupTags(g.id, "douban", 90, tags)
	if result.Images > imagesBefore {
		result.ImagePaths = append(result.ImagePaths, itemDir)
	}
	return nil
}

func (s *Service) scrapeGroup(ctx context.Context, c *tmdbClient, g *mediaGroup, result *Result) error {
	kind := "tv"
	queryTitle, year := g.info.Title, g.info.Year
	if g.info.Kind == strm.KindMovie {
		kind = "movie"
		queryTitle, year = strm.CleanMovieTitle(g.info.Title)
	} else {
		queryTitle = metadataQueryTitle(g.info.Title, g.resources[0].FilePath)
	}
	var found tmdbItem
	var err error
	var content *contentEvidence
	confidence, reason := 0, ""
	if detail, detailErr := s.store.GetMediaGroupDetail(g.id); detailErr == nil && detail.Group.DecisionSource == "manual" && detail.Group.SelectedSource == "tmdb" && detail.Group.SelectedID != "" {
		id, parseErr := strconv.Atoi(detail.Group.SelectedID)
		if parseErr == nil && id > 0 {
			found = tmdbItem{ID: id}
			confidence, reason = 100, "人工确认"
		}
	}
	if found.ID == 0 {
		found, confidence, reason, queryTitle, err = s.findTMDbMatch(ctx, c, g, kind)
		if err != nil {
			s.markPending(g, tmdbItem{}, 0, err.Error())
			if kind == "tv" {
				s.writeFallbackTVShow(g)
			}
			return err
		}
		year = firstNonZero(candidateYear(found), year)
		candidateLibrary := g.info.Library()
		if kind == "movie" {
			candidateLibrary = strm.LibMovies
		} else if containsInt(found.GenreIDs, tmdbAnimationGenreID) {
			candidateLibrary = strm.LibAnime
		}
		titleExact := confidence >= 75
		yearMatch := year > 0 && candidateYear(found) == year
		typeMatch := kind == "movie" || containsInt(found.GenreIDs, tmdbAnimationGenreID) == requiresAnimation(g.info)
		candidate := store.MediaCandidate{
			GroupID: g.id, Source: "tmdb", ProviderID: strconv.Itoa(found.ID), Title: firstTitle(found),
			OriginalTitle: firstOriginalTitle(found), Year: candidateYear(found), MediaKind: kind,
			Library: string(candidateLibrary), Score: confidence,
		}
		cachedAnalysis, cached, _ := s.store.GetContentAnalysis(g.resources[0].ID)
		contentReady := cached && cachedAnalysis.Status == contentAnalysisVersion
		if confidence < 98 && (contentReady || s.takeContentBudget()) {
			checked, _ := s.contentVerifier.Verify(ctx, VerificationRequest{
				Resource: g.resources[0], QueryTitle: queryTitle,
				CandidateTitle: firstTitle(found), ProviderID: strconv.Itoa(found.ID),
			})
			content = &checked
			contentJSON, _ := json.Marshal(checked)
			_ = s.store.RecordMediaVerification(store.MediaVerification{
				GroupID: g.id, ResourceID: g.resources[0].ID, Verifier: "content",
				VerifierVersion: contentAnalysisVersion, Status: checked.Status,
				ScoreDelta: checked.ScoreDelta, Reason: checked.Reason, EvidenceJSON: string(contentJSON),
			})
			switch checked.Status {
			case "supporting":
				confidence = min(100, confidence+checked.ScoreDelta)
				reason += "；" + checked.Reason
			case "conflicting":
				confidence = min(60, max(0, confidence+checked.ScoreDelta))
				reason += "；内容冲突：" + checked.Reason
			}
		}
		candidate.Score = confidence
		candidate.EvidenceJSON = evidenceJSONWithContent(reason, titleExact, yearMatch, typeMatch, content)
		s.persistCandidate(g, candidate)
		if confidence < 90 {
			s.markPending(g, found, confidence, reason)
			return fmt.Errorf("%w: %s", ErrNeedsReview, reason)
		}
	}
	detail, err := c.details(ctx, kind, found.ID)
	if err != nil {
		return err
	}
	name, original, premiered := detail.Name, detail.OriginalName, detail.FirstAirDate
	if kind == "movie" {
		name, original, premiered = detail.Title, detail.OriginalTitle, detail.ReleaseDate
	}
	if name == "" {
		name = queryTitle
	}
	// 刮削结果确认媒体类型后持久化分类，后续 STRM 重建优先使用该结果。
	confirmedLibrary := confirmedLibrary(kind, detail.Genres)
	s.persistCandidate(g, store.MediaCandidate{
		GroupID: g.id, Source: "tmdb", ProviderID: strconv.Itoa(detail.ID), Title: name,
		OriginalTitle: original, Year: yearOf(premiered), MediaKind: kind,
		Library: string(confirmedLibrary), Score: confidence, EvidenceJSON: evidenceJSONWithContent(reason, true, year == 0 || yearOf(premiered) == year, true, content),
	})
	s.setAutomaticDecision(g.id, store.MediaGroupDecision{
		Status: "verified", DecisionSource: "auto", SelectedSource: "tmdb", SelectedID: strconv.Itoa(detail.ID),
		CanonicalTitle: name, Library: string(confirmedLibrary), Confidence: confidence, Reason: reason,
	})
	s.learnAliases(g, queryTitle, name, original, kind, "tmdb", strconv.Itoa(detail.ID), yearOf(premiered), confidence)
	// NFO 和图片必须直接写入最终媒体库。若仍使用旧的路径分类，下一轮
	// STRM 重建虽然会移动播放文件，却会把真人海报留在动漫目录。
	itemDir, base := s.canonicalItemPath(g, name, confirmedLibrary)
	genres, studios, actors := namesGenres(detail.Genres), namesCompanies(detail.Networks), namesActors(detail.Credits.Cast)
	if kind == "movie" {
		studios = namesCompanies(detail.ProductionCompanies)
	}
	tags := []string{}
	if country := g.info.Country(); country != "" {
		tags = append(tags, country)
	}
	ids := []uniqueID{{Type: "tmdb", Default: true, Value: strconv.Itoa(detail.ID)}}
	if kind == "movie" {
		nfoPath := filepath.Join(itemDir, base+".nfo")
		err = s.publisher.WriteNFO(nfoPath, movieNFO{Title: name, OriginalTitle: original, Plot: detail.Overview, Year: yearOf(premiered), Premiered: premiered, Rating: detail.VoteAverage, UniqueIDs: ids, Genres: genres, Studios: studios, Actors: actors, Tags: tags, LockData: true})
		if err == nil {
			s.recordArtifact(g.id, "nfo", nfoPath, "ready", "")
		}
	} else {
		nfoPath := filepath.Join(itemDir, "tvshow.nfo")
		err = s.publisher.WriteNFO(nfoPath, tvshowNFO{Title: name, OriginalTitle: original, Plot: detail.Overview, Year: yearOf(premiered), Premiered: premiered, Rating: detail.VoteAverage, UniqueIDs: ids, Genres: genres, Studios: studios, Actors: actors, Tags: tags, LockData: true})
		if err == nil {
			s.recordArtifact(g.id, "nfo", nfoPath, "ready", "")
		}
	}
	if err != nil {
		return err
	}
	result.Items++
	imagesBefore := result.Images
	if detail.PosterPath != "" {
		posterPath := filepath.Join(itemDir, "poster.jpg")
		if err := s.publisher.PublishImage(ctx, c, detail.PosterPath, posterPath); err == nil {
			result.Images++
			s.recordArtifact(g.id, "poster", posterPath, "ready", "")
		} else {
			slog.Warn("TMDb 海报下载失败", "title", name, "path", detail.PosterPath, "error", err)
		}
	}
	if detail.BackdropPath != "" {
		fanartPath := filepath.Join(itemDir, "fanart.jpg")
		if err := s.publisher.PublishImage(ctx, c, detail.BackdropPath, fanartPath); err == nil {
			result.Images++
			s.recordArtifact(g.id, "fanart", fanartPath, "ready", "")
		} else {
			slog.Warn("TMDb 背景图下载失败", "title", name, "path", detail.BackdropPath, "error", err)
		}
	}
	if result.Images > imagesBefore {
		result.ImagePaths = append(result.ImagePaths, itemDir)
	}
	s.replaceGroupTags(g.id, "tmdb", confidence, append(append([]string{}, tags...), genres...))
	if kind == "movie" {
		return nil
	}
	seasons := map[int][]tmdbEpisode{}
	for _, r := range g.resources {
		info := s.analyzePath(r).Info
		if _, ok := seasons[info.Season]; !ok {
			eps, e := c.season(ctx, detail.ID, info.Season)
			if e != nil {
				if info.Season != 0 {
					return e
				}
				eps = []tmdbEpisode{}
			}
			seasons[info.Season] = eps
		}
		var matched *tmdbEpisode
		for i := range seasons[info.Season] {
			if seasons[info.Season][i].EpisodeNumber == info.Episode {
				matched = &seasons[info.Season][i]
				break
			}
		}
		epBase := fmt.Sprintf("%s S%02dE%02d", strm.Sanitize(g.info.Title), info.Season, info.Episode)
		epPath := filepath.Join(itemDir, fmt.Sprintf("Season %02d", info.Season), epBase+".nfo")
		thumbPath := filepath.Join(itemDir, fmt.Sprintf("Season %02d", info.Season), epBase+"-thumb.jpg")
		nfo := episodeNFO{Title: r.Title, Season: info.Season, Episode: info.Episode, LockData: true}
		if matched != nil {
			nfo = episodeNFO{Title: matched.Name, Plot: matched.Overview, Aired: matched.AirDate, Rating: matched.VoteAverage, Season: info.Season, Episode: info.Episode, UniqueIDs: []uniqueID{{Type: "tmdb", Default: true, Value: strconv.Itoa(matched.ID)}}, Actors: namesActors(matched.GuestStars), LockData: true}
		}
		if err := s.publisher.WriteNFO(epPath, nfo); err != nil {
			return err
		}
		s.recordArtifact(g.id, "episode_nfo", epPath, "ready", "")
		// 动画分集剧照在 TMDb 上可能混入同名真人作品，统一使用已校验的本剧背景。
		if requiresAnimation(g.info) || matched == nil || matched.StillPath == "" {
			if s.publisher.InstallFallbackImage(filepath.Join(itemDir, "fanart.jpg"), thumbPath) == nil {
				result.Images++
				s.recordArtifact(g.id, "episode_thumb", thumbPath, "ready", "")
			}
		} else if err := s.publisher.PublishImage(ctx, c, matched.StillPath, thumbPath); err == nil {
			result.Images++
			s.recordArtifact(g.id, "episode_thumb", thumbPath, "ready", "")
		}
		result.Episodes++
	}
	return nil
}

func requiresAnimation(info strm.MediaInfo) bool {
	if strm.ValidLibrary(string(info.OverrideLibrary)) {
		return info.OverrideLibrary == strm.LibAnime
	}
	return info.IsAnime()
}

func (s *Service) takeContentBudget() bool {
	for {
		left := s.contentBudget.Load()
		if left <= 0 {
			return false
		}
		if s.contentBudget.CompareAndSwap(left, left-1) {
			return true
		}
	}
}

// matchConfidence keeps the default evidence weight used by manual candidate search.
func matchConfidence(query string, year int, found tmdbItem) (int, string) {
	return matchConfidenceWithEvidence(query, year, found, 12, false)
}

// matchConfidenceWithEvidence combines independent evidence. A title or media
// type conflict cannot be compensated for by weaker signals.
func matchConfidenceWithEvidence(query string, year int, found tmdbItem, pathWeight int, requireAnimation bool) (int, string) {
	decision := (strictMatchEngine{}).Evaluate(MatchInput{
		Query:     MetadataQuery{Title: query, Year: year, RequireAnimation: requireAnimation},
		Candidate: metadataCandidateFromTMDb(found), PathWeight: pathWeight, PathAutoEligible: true,
	})
	return decision.Score, decision.Reason
}

func (s *Service) findTMDbMatch(ctx context.Context, c *tmdbClient, g *mediaGroup, kind string) (tmdbItem, int, string, string, error) {
	provider := tmdbProvider{client: c}
	analysis := s.analyzePath(g.resources[0])
	queries := s.titleCandidates(g.resources[0], analysis)
	if len(queries) == 0 {
		return tmdbItem{}, 0, "没有可靠片名", g.info.Title, errors.New("没有可用于 TMDb 搜索的可靠片名")
	}
	for _, query := range queries {
		aliases, aliasErr := s.store.FindMediaAliases(query.Title, kind, firstNonZero(query.Year, g.info.Year))
		if aliasErr == nil {
			for _, alias := range aliases {
				if alias.Source != "tmdb" || alias.Confidence < 90 {
					continue
				}
				id, parseErr := strconv.Atoi(alias.ProviderID)
				if parseErr == nil && id > 0 {
					return tmdbItem{ID: id}, alias.Confidence, "命中已确认作品别名", query.Title, nil
				}
			}
		}
	}

	var best tmdbItem
	bestScore, bestReason, bestQuery := 0, "", queries[0].Title
	var lastErr error
	for i, query := range queries {
		if i >= 6 {
			break
		}
		queryYear := firstNonZero(query.Year, g.info.Year)
		metadataResults, searchErr := provider.Search(ctx, MetadataQuery{
			Kind: kind, Title: query.Title, Year: queryYear, RequireAnimation: requiresAnimation(g.info),
		})
		if searchErr != nil {
			lastErr = searchErr
			continue
		}
		var found tmdbItem
		var selected MetadataCandidate
		for _, candidate := range metadataResults {
			item, ok := candidate.native.(tmdbItem)
			if !ok {
				continue
			}
			if picked, matches := pickSearchResult([]tmdbItem{item}, query.Title, queryYear, requiresAnimation(g.info)); matches {
				found, selected = picked, candidate
				break
			}
		}
		ok := found.ID > 0
		if !ok {
			lastErr = fmt.Errorf("TMDb 未找到同名作品 %q", query.Title)
			continue
		}
		decision := s.matchEngine.Evaluate(MatchInput{
			Query:     MetadataQuery{Kind: kind, Title: query.Title, Year: queryYear, RequireAnimation: requiresAnimation(g.info)},
			Candidate: selected, PathWeight: query.Weight, PathAutoEligible: query.AutoEligible,
		})
		score, matchReason := decision.Score, decision.Reason
		matchJSON, _ := json.Marshal(map[string]any{
			"query": query, "candidate": selected, "score": score,
		})
		verificationStatus := "neutral"
		if score >= 90 {
			verificationStatus = "supporting"
		} else if score == 0 || score == 20 {
			verificationStatus = "conflicting"
		}
		_ = s.store.RecordMediaVerification(store.MediaVerification{
			GroupID: g.id, ResourceID: g.resources[0].ID, Verifier: "metadata_match",
			VerifierVersion: "strict-v1", Status: verificationStatus,
			ScoreDelta: score, Reason: matchReason, EvidenceJSON: string(matchJSON),
		})
		if score > bestScore {
			best, bestScore, bestReason, bestQuery = found, score, matchReason+"；来源="+query.Source, query.Title
		}
		if score >= 90 {
			return found, score, bestReason, query.Title, nil
		}
	}
	if best.ID > 0 {
		return best, bestScore, bestReason, bestQuery, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("TMDb 未找到候选 %q", queries[0].Title)
	}
	return tmdbItem{}, 0, lastErr.Error(), queries[0].Title, lastErr
}

func (s *Service) learnAliases(g *mediaGroup, matchedTitle, canonicalTitle, originalTitle, kind, source, providerID string, year, confidence int) {
	for _, alias := range []string{matchedTitle, originalTitle} {
		if normalizeTitle(alias) == "" || normalizeTitle(alias) == normalizeTitle(canonicalTitle) {
			continue
		}
		_ = s.store.UpsertMediaAlias(store.MediaAlias{Alias: alias, CanonicalTitle: canonicalTitle,
			MediaKind: kind, Source: source, ProviderID: providerID, Year: year, Confidence: confidence})
	}
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func (s *Service) markPending(g *mediaGroup, found tmdbItem, confidence int, reason string) {
	s.setAutomaticDecision(g.id, store.MediaGroupDecision{
		Status: "review", DecisionSource: "auto", SelectedSource: "", SelectedID: "",
		CanonicalTitle: "", Library: string(strm.LibReview), Confidence: confidence, Reason: reason,
	})
}

func (s *Service) setAutomaticDecision(groupID int64, decision store.MediaGroupDecision) {
	if detail, err := s.store.GetMediaGroupDetail(groupID); err == nil && detail.Group.DecisionSource == "manual" {
		return
	}
	_ = s.store.SetMediaGroupDecision(groupID, decision)
}

func (s *Service) persistCandidate(g *mediaGroup, candidate store.MediaCandidate) {
	detail, err := s.store.GetMediaGroupDetail(g.id)
	if err != nil {
		return
	}
	candidates := detail.Candidates
	replaced := false
	for i := range candidates {
		if candidates[i].Source == candidate.Source && candidates[i].ProviderID == candidate.ProviderID {
			candidates[i] = candidate
			replaced = true
			break
		}
	}
	if !replaced {
		candidates = append(candidates, candidate)
	}
	_ = s.store.ReplaceMediaCandidates(g.id, candidates)
}

func evidenceJSON(summary string, titleExact, yearMatch, typeMatch bool) string {
	value := map[string]any{"summary": summary, "titleExact": titleExact, "yearMatch": yearMatch, "typeMatch": typeMatch}
	b, _ := json.Marshal(value)
	return string(b)
}

func evidenceJSONWithContent(summary string, titleExact, yearMatch, typeMatch bool, content *contentEvidence) string {
	value := map[string]any{"summary": summary, "titleExact": titleExact, "yearMatch": yearMatch, "typeMatch": typeMatch}
	if content != nil {
		value["content"] = content
	}
	b, _ := json.Marshal(value)
	return string(b)
}

func firstTitle(item tmdbItem) string {
	if item.Title != "" {
		return item.Title
	}
	return item.Name
}

func firstOriginalTitle(item tmdbItem) string {
	if item.OriginalTitle != "" {
		return item.OriginalTitle
	}
	return item.OriginalName
}

func candidateYear(item tmdbItem) int {
	if year := yearOf(item.ReleaseDate); year > 0 {
		return year
	}
	return yearOf(item.FirstAirDate)
}

func confirmedLibrary(kind string, genres []tmdbGenre) strm.LibraryKind {
	if kind == "movie" {
		return strm.LibMovies
	}
	for _, genre := range genres {
		switch genre.ID {
		case tmdbAnimationGenreID:
			return strm.LibAnime
		case 99, 10764, 10767:
			return strm.LibVariety
		}
	}
	return strm.LibTV
}

// installFallbackThumb 用本剧背景兜底没有剧照的分集，并使用硬链接避免重复占用空间。
func installFallbackThumb(fanartPath, thumbPath string) error {
	if _, err := os.Stat(fanartPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(thumbPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(thumbPath)
	return os.Link(fanartPath, thumbPath)
}

func (s *Service) recordArtifact(groupID int64, artifactType, artifactPath, status, errText string) {
	contentHash := ""
	if content, err := os.ReadFile(artifactPath); err == nil {
		digest := sha256.Sum256(content)
		contentHash = hex.EncodeToString(digest[:])
	}
	_ = s.store.RecordMediaPublicationArtifact(store.MediaPublicationArtifact{
		GroupID: groupID, ArtifactType: artifactType, Path: artifactPath,
		ContentHash: contentHash, Status: status, Error: errText,
	})
}

func (s *Service) replaceGroupTags(groupID int64, source string, confidence int, values []string) {
	seen := map[string]bool{}
	tags := make([]store.MediaTag, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := normalizeTitle(value)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		tags = append(tags, store.MediaTag{Name: value, Source: source, Confidence: confidence})
	}
	_ = s.store.ReplaceMediaGroupTags(groupID, tags)
}

func (s *Service) writeFallbackTVShow(g *mediaGroup) {
	itemDir, _ := s.itemPath(g.info, g.resources[0])
	_ = os.Remove(filepath.Join(itemDir, "poster.jpg"))
	_ = os.Remove(filepath.Join(itemDir, "fanart.jpg"))
	tags := []string{}
	if country := g.info.Country(); country != "" {
		tags = append(tags, country)
	}
	_ = s.publisher.WriteNFO(filepath.Join(itemDir, "tvshow.nfo"), tvshowNFO{
		Title: metadataQueryTitle(g.info.Title, g.resources[0].FilePath),
		Tags:  tags, LockData: true,
	})
}

var trailingSeriesNumber = regexp.MustCompile(`^(.+?)([1-9]\d?)$`)
var genericTechnicalTitle = regexp.MustCompile(`(?i)^(?:s\d{1,3}e\d{1,3}|\d{1,3}|4k|8k|720p|1080p|2160p|hdr|fullhd|uhd|web-dl|bluray|h264|h265|aac|後|集|话|話)$`)

// metadataQueryTitle 从合集路径里恢复主作品名，避免把季号或“番外”等目录拿去搜索。
func metadataQueryTitle(title, filePath string) string {
	title = strings.TrimSpace(title)
	segs := strings.Split(filepath.ToSlash(filePath), "/")
	parents := segs
	if len(parents) > 0 {
		parents = parents[:len(parents)-1]
	}
	if isGenericPart(title) {
		for i := len(parents) - 1; i >= 0; i-- {
			candidate := cleanCollectionSegment(parents[i])
			if candidate != "" && !isGenericPart(candidate) {
				return candidate
			}
		}
	}
	if match := trailingSeriesNumber.FindStringSubmatch(title); match != nil {
		base := strings.TrimSpace(match[1])
		for _, segment := range parents {
			if strings.Contains(segment, base) && strings.Contains(segment, "季") {
				return base
			}
		}
	}
	for _, segment := range parents {
		clean := cleanCollectionSegment(segment)
		if strings.Contains(clean, "+") && strings.Contains(clean, title) {
			if i := strings.Index(clean, " "); i > 0 {
				return strings.TrimSpace(clean[:i])
			}
		}
	}
	return title
}

func isGenericPart(value string) bool {
	value = strings.TrimSpace(value)
	if genericTechnicalTitle.MatchString(value) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "movie", "movies", "电影版", "剧场版", "番外", "特别篇", "special", "specials":
		return true
	}
	return false
}

func cleanCollectionSegment(value string) string {
	value = strings.TrimSpace(value)
	if ext := path.Ext(value); ext != "" && len(ext) <= 6 {
		value = strings.TrimSuffix(value, ext)
	}
	value = regexp.MustCompile(`^(?:19|20)\d{2}[._ -]+`).ReplaceAllString(value, "")
	if i := strings.Index(value, "."); i > 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

func (s *Service) itemPath(info strm.MediaInfo, r store.Resource) (string, string) {
	if info.Kind == strm.KindMovie {
		name, year := strm.CleanMovieTitle(info.Title)
		name = strm.Sanitize(name)
		if name == "" {
			name = fmt.Sprintf("resource-%d", r.ID)
		}
		folder := name
		if year > 0 {
			folder = fmt.Sprintf("%s (%d)", name, year)
		}
		return filepath.Join(s.mediaDir, "movies", folder), name
	}
	show := strm.Sanitize(info.Title)
	if show == "" {
		show = fmt.Sprintf("resource-%d", r.ID)
	}
	return filepath.Join(s.mediaDir, string(info.Library()), show), show
}

// canonicalItemPath ensures metadata and STRM generation resolve to the same
// directory after a provider has selected the canonical title and library.
func (s *Service) canonicalItemPath(g *mediaGroup, title string, library strm.LibraryKind) (string, string) {
	g.info.Title = strings.TrimSpace(title)
	g.info.OverrideLibrary = library
	return s.itemPath(g.info, g.resources[0])
}

func yearOf(date string) int {
	if len(date) >= 4 {
		n, _ := strconv.Atoi(date[:4])
		return n
	}
	return 0
}
func namesGenres(in []tmdbGenre) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v.Name != "" {
			out = append(out, v.Name)
		}
	}
	return out
}
func namesCompanies(in []tmdbCompany) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v.Name != "" {
			out = append(out, v.Name)
		}
	}
	return out
}
func namesActors(in []tmdbCast) []actor {
	if len(in) > 20 {
		in = in[:20]
	}
	out := make([]actor, 0, len(in))
	for _, v := range in {
		out = append(out, actor{Name: v.Name, Role: v.Character})
	}
	return out
}
