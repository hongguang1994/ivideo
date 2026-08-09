package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

func (s *sqlStore) SaveMediaPathAnalysis(analysis MediaPathAnalysis, candidates []MediaTitleCandidate) error {
	now := time.Now().Unix()
	analysis.AnalyzedAt = now
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if s.d.driver == "mysql" {
		_, err = tx.Exec(`INSERT INTO media_path_analyses (resource_id, analyzer_version, path_hash, analysis_json, analyzed_at) VALUES (?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE analyzer_version=VALUES(analyzer_version), path_hash=VALUES(path_hash), analysis_json=VALUES(analysis_json), analyzed_at=VALUES(analyzed_at)`,
			analysis.ResourceID, analysis.AnalyzerVersion, analysis.PathHash, analysis.AnalysisJSON, analysis.AnalyzedAt)
	} else {
		_, err = tx.Exec(`INSERT INTO media_path_analyses (resource_id, analyzer_version, path_hash, analysis_json, analyzed_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(resource_id) DO UPDATE SET analyzer_version=excluded.analyzer_version, path_hash=excluded.path_hash, analysis_json=excluded.analysis_json, analyzed_at=excluded.analyzed_at`,
			analysis.ResourceID, analysis.AnalyzerVersion, analysis.PathHash, analysis.AnalysisJSON, analysis.AnalyzedAt)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM media_title_candidates WHERE resource_id=?`, analysis.ResourceID); err != nil {
		return err
	}
	for _, candidate := range candidates {
		candidate.ResourceID = analysis.ResourceID
		candidate.CreatedAt = now
		if candidate.NormalizedTitle == "" {
			candidate.NormalizedTitle = normalizeLabel(candidate.Title)
		}
		if _, err := tx.Exec(`INSERT INTO media_title_candidates (resource_id, title, normalized_title, year, source, weight, auto_eligible, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			candidate.ResourceID, candidate.Title, candidate.NormalizedTitle, candidate.Year, candidate.Source, candidate.Weight, candidate.AutoEligible, candidate.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqlStore) GetMediaPathAnalysis(resourceID int64) (MediaPathAnalysis, []MediaTitleCandidate, bool, error) {
	var analysis MediaPathAnalysis
	err := s.db.QueryRow(`SELECT resource_id, analyzer_version, path_hash, analysis_json, analyzed_at FROM media_path_analyses WHERE resource_id=?`, resourceID).
		Scan(&analysis.ResourceID, &analysis.AnalyzerVersion, &analysis.PathHash, &analysis.AnalysisJSON, &analysis.AnalyzedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return MediaPathAnalysis{ResourceID: resourceID}, nil, false, nil
	}
	if err != nil {
		return analysis, nil, false, err
	}
	rows, err := s.db.Query(`SELECT id, resource_id, title, normalized_title, year, source, weight, auto_eligible, created_at FROM media_title_candidates WHERE resource_id=? ORDER BY weight DESC, id`, resourceID)
	if err != nil {
		return analysis, nil, true, err
	}
	defer rows.Close()
	var candidates []MediaTitleCandidate
	for rows.Next() {
		var candidate MediaTitleCandidate
		if err := rows.Scan(&candidate.ID, &candidate.ResourceID, &candidate.Title, &candidate.NormalizedTitle, &candidate.Year, &candidate.Source, &candidate.Weight, &candidate.AutoEligible, &candidate.CreatedAt); err != nil {
			return analysis, nil, true, err
		}
		candidates = append(candidates, candidate)
	}
	return analysis, candidates, true, rows.Err()
}

func (s *sqlStore) RecordMediaVerification(v MediaVerification) error {
	if v.CreatedAt == 0 {
		v.CreatedAt = time.Now().Unix()
	}
	if v.EvidenceJSON == "" {
		v.EvidenceJSON = "{}"
	}
	var groupID any
	if v.GroupID > 0 {
		groupID = v.GroupID
	}
	_, err := s.db.Exec(`INSERT INTO media_verifications (group_id, resource_id, verifier, verifier_version, status, score_delta, reason, evidence_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		groupID, v.ResourceID, v.Verifier, v.VerifierVersion, v.Status, v.ScoreDelta, v.Reason, v.EvidenceJSON, v.CreatedAt)
	return err
}

func (s *sqlStore) ListMediaVerifications(groupID int64) ([]MediaVerification, error) {
	rows, err := s.db.Query(`SELECT id, COALESCE(group_id,0), resource_id, verifier, verifier_version, status, score_delta, reason, evidence_json, created_at FROM media_verifications WHERE group_id=? ORDER BY created_at DESC, id DESC`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaVerification
	for rows.Next() {
		var v MediaVerification
		if err := rows.Scan(&v.ID, &v.GroupID, &v.ResourceID, &v.Verifier, &v.VerifierVersion, &v.Status, &v.ScoreDelta, &v.Reason, &v.EvidenceJSON, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *sqlStore) StartMediaProcessingJob(job MediaProcessingJob) (int64, error) {
	now := time.Now().Unix()
	if job.Kind == "" {
		job.Kind = "scrape"
	}
	if job.Status == "" {
		job.Status = "running"
	}
	if job.StartedAt == 0 {
		job.StartedAt = now
	}
	result, err := s.db.Exec(`INSERT INTO media_processing_jobs (kind, status, total_count, processed_count, failed_count, error, started_at, finished_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.Kind, job.Status, job.TotalCount, job.ProcessedCount, job.FailedCount, job.Error, job.StartedAt, job.FinishedAt, now, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *sqlStore) UpdateMediaProcessingJob(job MediaProcessingJob) error {
	if job.ID == 0 {
		return errors.New("媒体处理任务 ID 不能为空")
	}
	if job.Status == "completed" || job.Status == "failed" {
		if job.FinishedAt == 0 {
			job.FinishedAt = time.Now().Unix()
		}
	}
	_, err := s.db.Exec(`UPDATE media_processing_jobs SET status=?, total_count=?, processed_count=?, failed_count=?, error=?, finished_at=?, updated_at=? WHERE id=?`,
		job.Status, job.TotalCount, job.ProcessedCount, job.FailedCount, job.Error, job.FinishedAt, time.Now().Unix(), job.ID)
	return err
}

func (s *sqlStore) RecordMediaProcessingStep(step MediaProcessingStep) error {
	now := time.Now().Unix()
	if step.StartedAt == 0 {
		step.StartedAt = now
	}
	if step.Status == "completed" || step.Status == "failed" || step.Status == "review" {
		if step.FinishedAt == 0 {
			step.FinishedAt = now
		}
	}
	var groupID, resourceID any
	if step.GroupID > 0 {
		groupID = step.GroupID
	}
	if step.ResourceID > 0 {
		resourceID = step.ResourceID
	}
	_, err := s.db.Exec(`INSERT INTO media_processing_steps (job_id, group_id, resource_id, stage, status, error, started_at, finished_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		step.JobID, groupID, resourceID, step.Stage, step.Status, step.Error, step.StartedAt, step.FinishedAt)
	return err
}

func (s *sqlStore) RecordMediaPublicationArtifact(a MediaPublicationArtifact) error {
	if a.Status == "" {
		a.Status = "ready"
	}
	a.UpdatedAt = time.Now().Unix()
	if s.d.driver == "mysql" {
		_, err := s.db.Exec(`INSERT INTO media_publication_artifacts (group_id, artifact_type, path, content_hash, status, error, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE content_hash=VALUES(content_hash), status=VALUES(status), error=VALUES(error), updated_at=VALUES(updated_at)`,
			a.GroupID, a.ArtifactType, a.Path, a.ContentHash, a.Status, a.Error, a.UpdatedAt)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO media_publication_artifacts (group_id, artifact_type, path, content_hash, status, error, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(group_id, artifact_type, path) DO UPDATE SET content_hash=excluded.content_hash, status=excluded.status, error=excluded.error, updated_at=excluded.updated_at`,
		a.GroupID, a.ArtifactType, a.Path, a.ContentHash, a.Status, a.Error, a.UpdatedAt)
	return err
}

func (s *sqlStore) ReplaceMediaGroupTags(groupID int64, tags []MediaTag) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM media_group_tags WHERE group_id=? AND source <> 'manual'`, groupID); err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, tag := range tags {
		name := strings.TrimSpace(tag.Name)
		normalized := normalizeLabel(name)
		if normalized == "" {
			continue
		}
		var tagID int64
		if err := tx.QueryRow(`SELECT id FROM tags WHERE normalized_name=?`, normalized).Scan(&tagID); errors.Is(err, sql.ErrNoRows) {
			result, insertErr := tx.Exec(`INSERT INTO tags (name, normalized_name, created_at, updated_at) VALUES (?, ?, ?, ?)`, name, normalized, now, now)
			if insertErr != nil {
				return insertErr
			}
			tagID, err = result.LastInsertId()
		} else if err != nil {
			return err
		}
		source := tag.Source
		if source == "" {
			source = "metadata"
		}
		if _, err := tx.Exec(`INSERT INTO media_group_tags (group_id, tag_id, source, confidence, created_at) VALUES (?, ?, ?, ?, ?)`, groupID, tagID, source, tag.Confidence, now); err != nil {
			return fmt.Errorf("保存作品标签 %s: %w", name, err)
		}
	}
	return tx.Commit()
}

func (s *sqlStore) ListMediaGroupTags(groupID int64) ([]MediaTag, error) {
	rows, err := s.db.Query(`SELECT t.id, gt.group_id, t.name, t.normalized_name, gt.source, gt.confidence, gt.created_at FROM media_group_tags gt JOIN tags t ON t.id=gt.tag_id WHERE gt.group_id=? ORDER BY gt.confidence DESC, t.name`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaTag
	for rows.Next() {
		var tag MediaTag
		if err := rows.Scan(&tag.ID, &tag.GroupID, &tag.Name, &tag.Normalized, &tag.Source, &tag.Confidence, &tag.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

func normalizeLabel(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.TrimSpace(value))
}
