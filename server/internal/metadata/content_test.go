package metadata

import "testing"

func TestSignatureDistanceAllowsSmallEncodingDifferences(t *testing.T) {
	distance, frames, ok := signatureDistance(
		"0000000000000000:ffffffffffffffff:aaaaaaaaaaaaaaaa",
		"0000000000000003:fffffffffffffffc:aaaaaaaaaaaaaaa8",
	)
	if !ok || frames != 3 {
		t.Fatalf("signatureDistance() = %v, %d, %v", distance, frames, ok)
	}
	if distance != 5.0/3.0 {
		t.Fatalf("distance = %v, want %v", distance, 5.0/3.0)
	}
}

func TestSignatureDistanceRejectsIncompleteSignatures(t *testing.T) {
	if _, _, ok := signatureDistance("ffffffffffffffff", "ffffffffffffffff"); ok {
		t.Fatal("single-frame signatures must not be used as duplicate evidence")
	}
	if _, _, ok := signatureDistance("bad:hash", "bad:hash"); ok {
		t.Fatal("invalid signatures must be rejected")
	}
}

func TestDurationsComparable(t *testing.T) {
	if delta, ok := durationsComparable(1500, 1512); !ok || delta != 12 {
		t.Fatalf("near durations = %d, %v", delta, ok)
	}
	if _, ok := durationsComparable(1500, 1600); ok {
		t.Fatal("different durations must not be treated as the same content")
	}
}

func TestContentTitleMatchRequiresSpecificTitle(t *testing.T) {
	if contentTitleMatch("熊出", "熊出没之探险日记") {
		t.Fatal("short titles are too ambiguous for OCR evidence")
	}
	if !contentTitleMatch("熊出没之探险日记", "正在播放 熊出没之探险日记 第三集") {
		t.Fatal("exact long title should be accepted")
	}
	if contentTitleMatch("X", "X 2022") {
		t.Fatal("short latin titles are too ambiguous for OCR evidence")
	}
}

func TestSampleTimesStayInsideTypicalVideo(t *testing.T) {
	times := sampleTimes(1000)
	want := []float64{60, 220, 520}
	for i := range want {
		if times[i] != want[i] {
			t.Fatalf("sampleTimes()[%d] = %v, want %v", i, times[i], want[i])
		}
	}
}
