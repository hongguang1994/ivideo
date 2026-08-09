package metadata

import (
	"context"
	"testing"

	"ivideo/server/internal/store"
	"ivideo/server/internal/strm"
)

func TestStrictMatchEngineAcceptsIndependentEvidence(t *testing.T) {
	decision := (strictMatchEngine{}).Evaluate(MatchInput{
		Query: MetadataQuery{Kind: "movie", Title: "熊出没之变形记", Year: 2018, RequireAnimation: true},
		Candidate: MetadataCandidate{
			Title: "熊出没之变形记", Year: 2018, Animation: true, HasPoster: true,
		},
		PathWeight: 12, PathAutoEligible: true,
	})
	if decision.Score != 100 {
		t.Fatalf("score = %d, want 100 (%s)", decision.Score, decision.Reason)
	}
}

func TestStrictMatchEngineCapsParentOnlyCandidate(t *testing.T) {
	decision := (strictMatchEngine{}).Evaluate(MatchInput{
		Query: MetadataQuery{Kind: "movie", Title: "熊出没之变形记", Year: 2018, RequireAnimation: true},
		Candidate: MetadataCandidate{
			Title: "熊出没之变形记", Year: 2018, Animation: true, HasPoster: true,
		},
		PathWeight: 15, PathAutoEligible: false,
	})
	if decision.Score != 89 {
		t.Fatalf("score = %d, want 89 (%s)", decision.Score, decision.Reason)
	}
}

func TestStrictMatchEngineRejectsAnimationConflict(t *testing.T) {
	decision := (strictMatchEngine{}).Evaluate(MatchInput{
		Query:            MetadataQuery{Title: "同名作品", RequireAnimation: true},
		Candidate:        MetadataCandidate{Title: "同名作品", Animation: false},
		PathAutoEligible: true,
	})
	if decision.Score != 0 || decision.Reason != "动画属性冲突" {
		t.Fatalf("decision = %+v", decision)
	}
}

type fixedPathAnalyzer struct{ analysis strm.PathAnalysis }

func (a fixedPathAnalyzer) Analyze(store.Resource) strm.PathAnalysis { return a.analysis }

type neutralVerifier struct{}

func (neutralVerifier) Verify(context.Context, VerificationRequest) (VerificationResult, error) {
	return VerificationResult{Status: "neutral"}, nil
}

func TestPipelineStagesCanBeReplacedIndependently(t *testing.T) {
	want := strm.PathAnalysis{SpecificTitle: "测试作品"}
	service := New(nil, t.TempDir(), "", WithPathAnalyzer(fixedPathAnalyzer{analysis: want}), WithContentVerifier(neutralVerifier{}))
	got := service.analyzePath(store.Resource{Title: "ignored"})
	if got.SpecificTitle != want.SpecificTitle {
		t.Fatalf("analysis = %+v, want %+v", got, want)
	}
}
