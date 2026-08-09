// Package metadata implements the replaceable media identification pipeline:
// path analysis, provider search, strict matching, content verification and artifact publication.
package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"ivideo/server/internal/mediaidentity"
	"ivideo/server/internal/store"
	"ivideo/server/internal/strm"
)

const pathAnalyzerVersion = "semantic-v2"

// PathAnalyzer turns an original resource path into structured, explainable evidence.
type PathAnalyzer interface {
	Analyze(resource store.Resource) strm.PathAnalysis
}

// CandidateGenerator derives independently sourced title candidates from path evidence.
type CandidateGenerator interface {
	Generate(resource store.Resource, analysis strm.PathAnalysis) []strm.TitleCandidate
}

type MetadataQuery struct {
	Kind             string
	Title            string
	Year             int
	RequireAnimation bool
	AnimationKnown   bool
}

// MetadataCandidate is the provider-neutral shape consumed by the match engine.
type MetadataCandidate struct {
	Provider      string
	ProviderID    string
	Title         string
	OriginalTitle string
	Year          int
	Animation     bool
	HasPoster     bool
	native        any
}

// MetadataProvider is the extension point for TMDb, Douban and future anime providers.
type MetadataProvider interface {
	Name() string
	Search(ctx context.Context, query MetadataQuery) ([]MetadataCandidate, error)
}

type MatchInput struct {
	Query            MetadataQuery
	Candidate        MetadataCandidate
	PathWeight       int
	PathAutoEligible bool
}

type MatchDecision struct {
	Score  int
	Reason string
}

// MatchEngine contains policy only. It performs no network or filesystem access.
type MatchEngine interface {
	Evaluate(input MatchInput) MatchDecision
}

type VerificationRequest struct {
	Resource       store.Resource
	QueryTitle     string
	CandidateTitle string
	ProviderID     string
}

type VerificationResult struct {
	Status            string  `json:"status"`
	ScoreDelta        int     `json:"scoreDelta"`
	OCRMatch          bool    `json:"ocrMatch"`
	SimilarResourceID int64   `json:"similarResourceId,omitempty"`
	FrameDistance     float64 `json:"frameDistance,omitempty"`
	DurationDelta     int     `json:"durationDelta,omitempty"`
	Reason            string  `json:"reason"`
}

// ContentVerifier contributes supporting or conflicting evidence without publishing files.
type ContentVerifier interface {
	Verify(ctx context.Context, request VerificationRequest) (VerificationResult, error)
}

type ArtworkDownloader interface {
	download(ctx context.Context, source, destination string) error
}

// MediaPublisher owns local media artifacts. Higher-level publication decisions stay
// in the orchestrator, while filesystem details remain replaceable and testable.
type MediaPublisher interface {
	WriteNFO(path string, value any) error
	PublishImage(ctx context.Context, downloader ArtworkDownloader, source, destination string) error
	InstallFallbackImage(sourcePath, destinationPath string) error
}

// Option replaces one pipeline stage while preserving the default orchestration.
type Option func(*Service)

func WithPathAnalyzer(analyzer PathAnalyzer) Option {
	return func(s *Service) {
		if analyzer != nil {
			s.pathAnalyzer = analyzer
		}
	}
}

func WithCandidateGenerator(generator CandidateGenerator) Option {
	return func(s *Service) {
		if generator != nil {
			s.candidateGenerator = generator
		}
	}
}

func WithIdentityResolver(resolver mediaidentity.Resolver) Option {
	return func(s *Service) {
		if resolver != nil {
			s.identityResolver = resolver
		}
	}
}

func WithMatchEngine(engine MatchEngine) Option {
	return func(s *Service) {
		if engine != nil {
			s.matchEngine = engine
		}
	}
}

func WithContentVerifier(verifier ContentVerifier) Option {
	return func(s *Service) {
		if verifier != nil {
			s.contentVerifier = verifier
		}
	}
}

func WithMediaPublisher(publisher MediaPublisher) Option {
	return func(s *Service) {
		if publisher != nil {
			s.publisher = publisher
		}
	}
}

type semanticPathAnalyzer struct{}

func (semanticPathAnalyzer) Analyze(resource store.Resource) strm.PathAnalysis {
	return strm.AnalyzeMediaPath(resource.FilePath, resource.Title)
}

type semanticCandidateGenerator struct{}

func (semanticCandidateGenerator) Generate(resource store.Resource, analysis strm.PathAnalysis) []strm.TitleCandidate {
	if len(analysis.Candidates) > 0 {
		return analysis.Candidates
	}
	return strm.MediaTitleCandidates(resource.FilePath, resource.Title, analysis.Info)
}

type strictMatchEngine struct{}

func (strictMatchEngine) Evaluate(input MatchInput) MatchDecision {
	want := normalizeTitle(input.Query.Title)
	if want == "" || (normalizeTitle(input.Candidate.Title) != want && normalizeTitle(input.Candidate.OriginalTitle) != want) {
		return MatchDecision{Score: 20, Reason: "候选标题与资源标题不完全一致"}
	}
	if (input.Query.AnimationKnown || input.Query.RequireAnimation) && input.Query.RequireAnimation != input.Candidate.Animation {
		return MatchDecision{Score: 0, Reason: "动画属性冲突"}
	}
	score := 65 // 标题 50 + 媒体类型 15
	switch {
	case input.Query.Year > 0 && input.Candidate.Year == input.Query.Year:
		score += 20
	case input.Query.Year == 0 || input.Candidate.Year == 0:
		score += 8
	default:
		return MatchDecision{Score: 20, Reason: fmt.Sprintf("年份不一致：资源 %d，候选 %d", input.Query.Year, input.Candidate.Year)}
	}
	score += max(0, min(input.PathWeight, 15))
	if input.Candidate.HasPoster {
		score += 5
	}
	score = min(score, 100)
	if !input.PathAutoEligible && score >= 90 {
		return MatchDecision{Score: 89, Reason: "父目录与明确片名不一致，只能作为人工候选"}
	}
	if score >= 90 {
		return MatchDecision{Score: score, Reason: "标题、类型及路径证据一致"}
	}
	return MatchDecision{Score: score, Reason: "证据不足，需人工确认"}
}

type serviceContentVerifier struct{ service *Service }

func (v serviceContentVerifier) Verify(ctx context.Context, request VerificationRequest) (VerificationResult, error) {
	return v.service.evaluateContentEvidence(ctx, request.Resource, request.QueryTitle, request.CandidateTitle, request.ProviderID)
}

type localMediaPublisher struct{}

func (localMediaPublisher) WriteNFO(path string, value any) error {
	return writeNFO(path, value)
}

func (localMediaPublisher) PublishImage(ctx context.Context, downloader ArtworkDownloader, source, destination string) error {
	return downloader.download(ctx, source, destination)
}

func (localMediaPublisher) InstallFallbackImage(sourcePath, destinationPath string) error {
	return installFallbackThumb(sourcePath, destinationPath)
}

type tmdbProvider struct{ client *tmdbClient }

func (p tmdbProvider) Name() string { return "tmdb" }

func (p tmdbProvider) Search(ctx context.Context, query MetadataQuery) ([]MetadataCandidate, error) {
	items, err := p.client.searchResults(ctx, query.Kind, query.Title, query.Year)
	if err != nil {
		return nil, err
	}
	out := make([]MetadataCandidate, 0, len(items))
	for _, item := range items {
		out = append(out, metadataCandidateFromTMDb(item))
	}
	return out, nil
}

func metadataCandidateFromTMDb(item tmdbItem) MetadataCandidate {
	return MetadataCandidate{
		Provider: "tmdb", ProviderID: strconv.Itoa(item.ID), Title: firstTitle(item),
		OriginalTitle: firstOriginalTitle(item), Year: candidateYear(item),
		Animation: containsInt(item.GenreIDs, tmdbAnimationGenreID), HasPoster: item.PosterPath != "", native: item,
	}
}

type doubanProvider struct{ client *doubanClient }

func (p doubanProvider) Name() string { return "douban" }

func (p doubanProvider) Search(ctx context.Context, query MetadataQuery) ([]MetadataCandidate, error) {
	item, err := p.client.search(ctx, query.Title, query.Year)
	if err != nil {
		return nil, err
	}
	return []MetadataCandidate{{
		Provider: "douban", ProviderID: item.ID, Title: item.Title, OriginalTitle: item.OriginalTitle,
		Year: item.Year, HasPoster: item.Poster != "", native: item,
	}}, nil
}

func (s *Service) analyzePath(resource store.Resource) strm.PathAnalysis {
	analysis := s.pathAnalyzer.Analyze(resource)
	candidates := make([]mediaidentity.Candidate, 0, len(analysis.Candidates))
	for _, candidate := range analysis.Candidates {
		candidates = append(candidates, mediaidentity.Candidate{Title: candidate.Title, Year: candidate.Year, Source: candidate.Source, Weight: candidate.Weight, AutoEligible: candidate.AutoEligible})
	}
	decision := s.identityResolver.Resolve(mediaidentity.Input{
		SourceTitle: resource.SourceTitle, SourceCategory: resource.SourceCategory,
		ResourceTitle: resource.Title, FilePath: resource.FilePath,
		ParsedKind: string(analysis.Info.Kind), ParsedTitle: analysis.Info.Title,
		Season: analysis.Info.Season, Episode: analysis.Info.Episode, Candidates: candidates,
	})
	analysis.IdentityTitle, analysis.IdentityStatus = decision.Title, decision.Status
	analysis.IdentityReason, analysis.IdentityConfidence = decision.Reason, decision.Confidence
	if decision.Title != "" {
		identityCandidate := strm.TitleCandidate{Title: decision.Title, Source: "来源标题", Weight: min(15, max(1, decision.Confidence/6)), AutoEligible: decision.AutoEligible}
		merged := []strm.TitleCandidate{identityCandidate}
		for _, candidate := range analysis.Candidates {
			if normalizeTitle(candidate.Title) != normalizeTitle(decision.Title) {
				merged = append(merged, candidate)
			}
		}
		analysis.Candidates = merged
	}
	if decision.Kind == string(strm.KindEpisode) && decision.Title != "" && decision.Confidence >= 90 {
		analysis.Info.Kind, analysis.Info.Title = strm.KindEpisode, decision.Title
		analysis.Info.Season, analysis.Info.Episode = decision.Season, decision.Episode
		analysis.SeriesTitle = decision.Title
	}
	if decision.AnimationHint == "animation" {
		analysis.Info.AnimationKnown = true
		analysis.Info.OverrideLibrary = strm.LibAnime
	} else if decision.AnimationHint == "live_action" {
		analysis.Info.AnimationKnown = true
	}
	return analysis
}

func (s *Service) titleCandidates(resource store.Resource, analysis strm.PathAnalysis) []strm.TitleCandidate {
	return s.candidateGenerator.Generate(resource, analysis)
}

func (s *Service) persistPathEvidence(resource store.Resource, analysis strm.PathAnalysis) error {
	payload, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(resource.FilePath))
	candidates := make([]store.MediaTitleCandidate, 0, len(analysis.Candidates))
	for _, candidate := range analysis.Candidates {
		candidates = append(candidates, store.MediaTitleCandidate{
			ResourceID: resource.ID, Title: candidate.Title, Year: candidate.Year,
			Source: candidate.Source, Weight: candidate.Weight, AutoEligible: candidate.AutoEligible,
		})
	}
	return s.store.SaveMediaPathAnalysis(store.MediaPathAnalysis{
		ResourceID: resource.ID, AnalyzerVersion: pathAnalyzerVersion,
		PathHash: hex.EncodeToString(digest[:]), AnalysisJSON: string(payload),
	}, candidates)
}
