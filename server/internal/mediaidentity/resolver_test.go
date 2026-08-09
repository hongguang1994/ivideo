package mediaidentity

import "testing"

func TestSourceTitleAndNumberedFileIdentifyEpisode(t *testing.T) {
	decision := (DefaultResolver{}).Resolve(Input{
		SourceTitle: "师兄啊师兄", FilePath: "/S🐻/146 4K.mp4", ParsedKind: "movie", ParsedTitle: "146 4K",
	})
	if decision.Status != "identified" || decision.Title != "师兄啊师兄" || decision.Kind != "episode" || decision.Season != 1 || decision.Episode != 146 || decision.Confidence < 90 || !decision.AutoEligible {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestSourceTitleDoesNotTurnNamedMovieIntoEpisode(t *testing.T) {
	decision := (DefaultResolver{}).Resolve(Input{
		SourceTitle: "诺兰电影合集", FilePath: "/movies/盗梦空间.2010.mkv", ParsedKind: "movie", ParsedTitle: "盗梦空间",
		Candidates: []Candidate{{Title: "盗梦空间", Source: "文件名", Weight: 15, AutoEligible: true}},
	})
	if decision.Kind != "movie" || decision.Episode != 0 {
		t.Fatalf("named movie was misclassified: %+v", decision)
	}
	if decision.AutoEligible {
		t.Fatalf("collection title must remain review-only: %+v", decision)
	}
}

func TestSourceCategoryProvidesAnimationHint(t *testing.T) {
	decision := (DefaultResolver{}).Resolve(Input{SourceTitle: "测试作品", SourceCategory: "国漫", FilePath: "/01.mp4"})
	if decision.AnimationHint != "animation" {
		t.Fatalf("missing animation hint: %+v", decision)
	}
}
