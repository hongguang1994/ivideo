package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"math/bits"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"ivideo/server/internal/store"
)

type probeOutput struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Tags      struct {
			Language string `json:"language"`
		} `json:"tags"`
	} `json:"streams"`
}

const contentAnalysisVersion = "ready-v2"

type contentEvidence = VerificationResult

func (s *Service) analyzeContent(ctx context.Context, resource store.Resource) (store.ContentAnalysis, error) {
	if cached, ok, err := s.store.GetContentAnalysis(resource.ID); err == nil && ok && cached.Status == contentAnalysisVersion {
		return cached, nil
	}
	analysis := store.ContentAnalysis{ResourceID: resource.ID, Status: "failed"}
	if s.siteURL == "" {
		return analysis, fmt.Errorf("未配置站点地址")
	}
	mediaURL := strings.TrimRight(s.siteURL, "/") + "/api/v1/file/" + strconv.FormatInt(resource.ID, 10) + path.Ext(resource.FilePath)
	if _, err := url.ParseRequestURI(mediaURL); err != nil {
		return analysis, err
	}

	probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	probe, err := exec.CommandContext(probeCtx, "ffprobe", "-v", "error", "-show_entries", "format=duration:stream=codec_type,codec_name,width,height:stream_tags=language", "-of", "json", mediaURL).Output()
	if err != nil {
		analysis.Error = "读取视频信息失败: " + err.Error()
		_ = s.store.SetContentAnalysis(analysis)
		return analysis, fmt.Errorf("%s", analysis.Error)
	}
	var info probeOutput
	if err := json.Unmarshal(probe, &info); err != nil {
		return analysis, err
	}
	duration, _ := strconv.ParseFloat(info.Format.Duration, 64)
	analysis.DurationSeconds = int(math.Round(duration))
	var languages []string
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && analysis.VideoCodec == "" {
			analysis.VideoCodec, analysis.Width, analysis.Height = stream.CodecName, stream.Width, stream.Height
		}
		if stream.CodecType == "audio" && stream.Tags.Language != "" {
			languages = append(languages, stream.Tags.Language)
		}
	}
	analysis.AudioLanguages = strings.Join(languages, ",")

	tmpDir, err := os.MkdirTemp("", "ivideo-content-")
	if err != nil {
		return analysis, err
	}
	defer os.RemoveAll(tmpDir)
	times := sampleTimes(duration)
	var hashes, ocr []string
	for i, second := range times {
		framePath := path.Join(tmpDir, fmt.Sprintf("frame-%d.png", i))
		frameCtx, frameCancel := context.WithTimeout(ctx, 45*time.Second)
		cmd := exec.CommandContext(frameCtx, "ffmpeg", "-loglevel", "error", "-ss", fmt.Sprintf("%.1f", second), "-i", mediaURL, "-frames:v", "1", "-vf", "scale=960:-2", "-y", framePath)
		frameErr := cmd.Run()
		frameCancel()
		if frameErr != nil {
			continue
		}
		if hash, err := perceptualHash(framePath); err == nil {
			hashes = append(hashes, hash)
		}
		ocrCtx, ocrCancel := context.WithTimeout(ctx, 20*time.Second)
		text, ocrErr := exec.CommandContext(ocrCtx, "tesseract", framePath, "stdout", "-l", "chi_sim+eng", "--psm", "6").Output()
		ocrCancel()
		if ocrErr == nil && strings.TrimSpace(string(text)) != "" {
			ocr = append(ocr, strings.TrimSpace(string(text)))
		}
	}
	analysis.FrameSignature = strings.Join(hashes, ":")
	analysis.OCRText = strings.Join(ocr, "\n")
	analysis.Status, analysis.Error = contentAnalysisVersion, ""
	if err := s.store.SetContentAnalysis(analysis); err != nil {
		return analysis, err
	}
	return analysis, nil
}

func sampleTimes(duration float64) []float64 {
	if duration <= 0 {
		return []float64{15, 60, 120}
	}
	return []float64{math.Max(2, duration*.06), math.Max(4, duration*.22), math.Max(6, duration*.52)}
}

func perceptualHash(framePath string) (string, error) {
	f, err := os.Open(framePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return "", err
	}
	b := img.Bounds()
	var hash uint64
	bit := uint(0)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			left := grayAt(img, b.Min.X+(x*b.Dx())/9, b.Min.Y+(y*b.Dy())/8)
			right := grayAt(img, b.Min.X+((x+1)*b.Dx())/9, b.Min.Y+(y*b.Dy())/8)
			if left > right {
				hash |= uint64(1) << bit
			}
			bit++
		}
	}
	return fmt.Sprintf("%016x", hash), nil
}

func grayAt(img interface{ At(int, int) color.Color }, x, y int) uint32 {
	r, g, b, _ := img.At(x, y).RGBA()
	return (299*r + 587*g + 114*b) / 1000
}

func contentTitleMatch(title, ocrText string) bool {
	want := normalizeTitle(title)
	got := normalizeTitle(ocrText)
	minimum := 4
	if isASCII(want) {
		minimum = 5
	}
	return len([]rune(want)) >= minimum && strings.Contains(got, want)
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}

func signatureDistance(left, right string) (float64, int, bool) {
	a, b := strings.Split(left, ":"), strings.Split(right, ":")
	if len(a) < 2 || len(a) != len(b) {
		return 0, 0, false
	}
	total := 0
	for i := range a {
		av, errA := strconv.ParseUint(a[i], 16, 64)
		bv, errB := strconv.ParseUint(b[i], 16, 64)
		if errA != nil || errB != nil {
			return 0, 0, false
		}
		total += bits.OnesCount64(av ^ bv)
	}
	return float64(total) / float64(len(a)), len(a), true
}

func durationsComparable(left, right int) (int, bool) {
	delta := left - right
	if delta < 0 {
		delta = -delta
	}
	if left <= 0 || right <= 0 {
		return delta, false
	}
	tolerance := int(math.Max(15, float64(max(left, right))*.03))
	return delta, delta <= tolerance
}

func (s *Service) evaluateContentEvidence(ctx context.Context, resource store.Resource, queryTitle, candidateTitle, providerID string) (contentEvidence, error) {
	analysis, err := s.analyzeContent(ctx, resource)
	if err != nil {
		return contentEvidence{Status: "unavailable", Reason: err.Error()}, err
	}
	evidence := contentEvidence{Status: "neutral", Reason: "已读取视频内容，尚未发现足以确认作品的证据"}
	if contentTitleMatch(queryTitle, analysis.OCRText) || contentTitleMatch(candidateTitle, analysis.OCRText) {
		evidence.Status = "supporting"
		evidence.OCRMatch = true
		evidence.ScoreDelta = 15
		evidence.Reason = "片中文字与作品名一致"
	}

	analyses, listErr := s.store.ListContentAnalyses(resource.ID)
	if listErr != nil {
		return evidence, nil
	}
	bestDistance := math.MaxFloat64
	var best store.ContentAnalysis
	bestDurationDelta := 0
	for _, other := range analyses {
		durationDelta, comparable := durationsComparable(analysis.DurationSeconds, other.DurationSeconds)
		if !comparable {
			continue
		}
		distance, frames, ok := signatureDistance(analysis.FrameSignature, other.FrameSignature)
		if !ok || frames < 3 || distance > 8 || distance >= bestDistance {
			continue
		}
		match, found, matchErr := s.store.GetResourceMediaDecision(other.ResourceID)
		if matchErr != nil || !found || match.ProviderID == "" || match.Status == "pending" || match.Confidence < 90 {
			continue
		}
		bestDistance, best, bestDurationDelta = distance, other, durationDelta
	}
	if best.ResourceID == 0 {
		return evidence, nil
	}
	match, found, _ := s.store.GetResourceMediaDecision(best.ResourceID)
	if !found {
		return evidence, nil
	}
	evidence.SimilarResourceID = best.ResourceID
	evidence.FrameDistance = math.Round(bestDistance*10) / 10
	evidence.DurationDelta = bestDurationDelta
	if match.ProviderID != providerID {
		evidence.Status = "conflicting"
		evidence.ScoreDelta = -40
		evidence.Reason = fmt.Sprintf("视频画面与已确认的《%s》高度相似，和当前候选冲突", match.Title)
		return evidence, nil
	}
	evidence.Status = "supporting"
	if evidence.ScoreDelta < 25 {
		evidence.ScoreDelta = 25
	}
	evidence.Reason = fmt.Sprintf("视频画面与已确认的《%s》高度相似", match.Title)
	return evidence, nil
}
