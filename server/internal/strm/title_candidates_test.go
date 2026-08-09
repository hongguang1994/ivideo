package strm

import "testing"

func TestMediaTitleCandidatesPreferMeaningfulParent(t *testing.T) {
	info := ParsePath("/国漫/2011-2019/2011.罗小黑战记.全集+电影/movie/【2019】4K.60帧.小黑.mkv", "【2019】4K.60帧.小黑")
	candidates := MediaTitleCandidates("/国漫/2011-2019/2011.罗小黑战记.全集+电影/movie/【2019】4K.60帧.小黑.mkv", "【2019】4K.60帧.小黑", info)
	if !hasTitleCandidate(candidates, "罗小黑战记") {
		t.Fatalf("missing parent title candidate: %+v", candidates)
	}
	for _, candidate := range candidates {
		if candidate.Title == "4K" || candidate.Title == "2011-2019" || candidate.Title == "movie" {
			t.Fatalf("technical/category title leaked into candidates: %+v", candidates)
		}
	}
}

func TestMediaTitleCandidatesUseParentForTechnicalFilename(t *testing.T) {
	filePath := "/电影/熊出没之奇幻空间/4K/1080P.mkv"
	info := ParsePath(filePath, "1080P")
	candidates := MediaTitleCandidates(filePath, "1080P", info)
	if !hasTitleCandidate(candidates, "熊出没之奇幻空间") {
		t.Fatalf("missing meaningful parent: %+v", candidates)
	}
	if hasTitleCandidate(candidates, "1080P") || hasTitleCandidate(candidates, "4K") {
		t.Fatalf("technical title should be filtered: %+v", candidates)
	}
}

func TestMediaTitleCandidatesAddDeobfuscatedChineseClue(t *testing.T) {
	info := MediaInfo{Title: "4K", Kind: KindMovie}
	candidates := MediaTitleCandidates("/Y英X雄S三Y元L里2026/4K.mp4", "4K", info)
	if len(candidates) < 2 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].Title != "Y英X雄S三Y元L里" || candidates[1].Title != "英雄三元里" {
		t.Fatalf("unexpected candidates: %#v", candidates[:2])
	}
	if candidates[1].Year != 2026 || candidates[1].Source != "父目录去混淆" {
		t.Fatalf("unexpected deobfuscated evidence: %#v", candidates[1])
	}
}

func TestMediaTitleCandidatesKeepLegitimateMixedTitleFirst(t *testing.T) {
	info := MediaInfo{Title: "X战警", Kind: KindMovie}
	candidates := MediaTitleCandidates("/X战警/X战警.mp4", "X战警", info)
	if len(candidates) == 0 || candidates[0].Title != "X战警" {
		t.Fatalf("original mixed title must stay first: %#v", candidates)
	}
}

func TestDeobfuscateIndexedTitleHandlesRepeatedPrefix(t *testing.T) {
	if got := deobfuscateIndexedTitle("ZZ战D刀T屠L狼2_2026"); got != "战刀屠狼2_2026" {
		t.Fatalf("deobfuscateIndexedTitle() = %q", got)
	}
}

func TestSpecificSeriesDoesNotAutoUseFranchiseParent(t *testing.T) {
	filePath := "/国漫/2012.熊出没.1-12部/熊熊乐园/01.mp4"
	info := ParsePath(filePath, "01")
	candidates := MediaTitleCandidates(filePath, "01", info)
	for _, candidate := range candidates {
		if candidate.Title == "熊出没" && candidate.AutoEligible {
			t.Fatalf("franchise parent must not auto publish a distinct series: %+v", candidates)
		}
	}
}

func TestNumberedSeasonMayUseBaseParent(t *testing.T) {
	filePath := "/国漫/十万个冷笑话/十万个冷笑话2/01.mp4"
	info := ParsePath(filePath, "01")
	candidates := MediaTitleCandidates(filePath, "01", info)
	found := false
	for _, candidate := range candidates {
		if candidate.Title == "十万个冷笑话" {
			found = candidate.AutoEligible
		}
	}
	if !found {
		t.Fatalf("numbered season should allow its base parent: %+v", candidates)
	}
}

func hasTitleCandidate(items []TitleCandidate, title string) bool {
	for _, item := range items {
		if item.Title == title {
			return true
		}
	}
	return false
}
