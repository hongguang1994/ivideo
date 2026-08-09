package store

import (
	"database/sql"
	"time"
)

const contentAnalysisColumns = `resource_id, duration_seconds, width, height, video_codec, audio_languages, frame_signature, ocr_text, status, error, analyzed_at`

func scanContentAnalysis(row interface{ Scan(...any) error }) (ContentAnalysis, error) {
	var a ContentAnalysis
	err := row.Scan(&a.ResourceID, &a.DurationSeconds, &a.Width, &a.Height, &a.VideoCodec, &a.AudioLanguages, &a.FrameSignature, &a.OCRText, &a.Status, &a.Error, &a.AnalyzedAt)
	return a, err
}

func (s *sqlStore) GetContentAnalysis(resourceID int64) (ContentAnalysis, bool, error) {
	a, err := scanContentAnalysis(s.db.QueryRow(`SELECT `+contentAnalysisColumns+` FROM media_content_analysis WHERE resource_id = ?`, resourceID))
	if err == sql.ErrNoRows {
		return ContentAnalysis{ResourceID: resourceID}, false, nil
	}
	return a, err == nil, err
}

func (s *sqlStore) SetContentAnalysis(a ContentAnalysis) error {
	a.AnalyzedAt = time.Now().Unix()
	if a.Status == "" {
		a.Status = "ready"
	}
	if s.d.driver == "mysql" {
		_, err := s.db.Exec(`INSERT INTO media_content_analysis (`+contentAnalysisColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE duration_seconds=VALUES(duration_seconds), width=VALUES(width), height=VALUES(height), video_codec=VALUES(video_codec), audio_languages=VALUES(audio_languages), frame_signature=VALUES(frame_signature), ocr_text=VALUES(ocr_text), status=VALUES(status), error=VALUES(error), analyzed_at=VALUES(analyzed_at)`,
			a.ResourceID, a.DurationSeconds, a.Width, a.Height, a.VideoCodec, a.AudioLanguages, a.FrameSignature, a.OCRText, a.Status, a.Error, a.AnalyzedAt)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO media_content_analysis (`+contentAnalysisColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(resource_id) DO UPDATE SET duration_seconds=excluded.duration_seconds, width=excluded.width, height=excluded.height, video_codec=excluded.video_codec, audio_languages=excluded.audio_languages, frame_signature=excluded.frame_signature, ocr_text=excluded.ocr_text, status=excluded.status, error=excluded.error, analyzed_at=excluded.analyzed_at`,
		a.ResourceID, a.DurationSeconds, a.Width, a.Height, a.VideoCodec, a.AudioLanguages, a.FrameSignature, a.OCRText, a.Status, a.Error, a.AnalyzedAt)
	return err
}

func (s *sqlStore) FindContentAnalysisBySignature(signature string, excludeResourceID int64) (ContentAnalysis, bool, error) {
	if signature == "" {
		return ContentAnalysis{}, false, nil
	}
	a, err := scanContentAnalysis(s.db.QueryRow(`SELECT `+contentAnalysisColumns+` FROM media_content_analysis WHERE frame_signature = ? AND resource_id <> ? AND status LIKE 'ready%' LIMIT 1`, signature, excludeResourceID))
	if err == sql.ErrNoRows {
		return ContentAnalysis{}, false, nil
	}
	return a, err == nil, err
}

func (s *sqlStore) ListContentAnalyses(excludeResourceID int64) ([]ContentAnalysis, error) {
	rows, err := s.db.Query(`SELECT `+contentAnalysisColumns+` FROM media_content_analysis WHERE resource_id <> ? AND status LIKE 'ready%' AND frame_signature <> ''`, excludeResourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var analyses []ContentAnalysis
	for rows.Next() {
		a, scanErr := scanContentAnalysis(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		analyses = append(analyses, a)
	}
	return analyses, rows.Err()
}
