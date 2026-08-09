package store

import (
	"database/sql"
	"fmt"
	"time"
)

const mediaGroupColumns = `id, group_key, raw_title, normalized_title, media_kind, suggested_library,
	year, status, decision_source, selected_source, selected_id, canonical_title, confidence, reason, created_at, updated_at`

func scanMediaGroup(sc rowScanner) (MediaGroup, error) {
	var g MediaGroup
	err := sc.Scan(&g.ID, &g.GroupKey, &g.RawTitle, &g.NormalizedTitle, &g.MediaKind,
		&g.SuggestedLibrary, &g.Year, &g.Status, &g.DecisionSource, &g.SelectedSource,
		&g.SelectedID, &g.CanonicalTitle, &g.Confidence, &g.Reason, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}

func (s *sqlStore) SyncMediaGroup(group MediaGroup, members []MediaGroupMember) (int64, error) {
	now := time.Now().Unix()
	if group.Status == "" {
		group.Status = "grouped"
	}
	if group.DecisionSource == "" {
		group.DecisionSource = "auto"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if s.d.driver == "mysql" {
		_, err = tx.Exec(`INSERT INTO media_groups (group_key, raw_title, normalized_title, media_kind, suggested_library, year, status, decision_source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE raw_title=VALUES(raw_title), normalized_title=VALUES(normalized_title), media_kind=VALUES(media_kind), suggested_library=VALUES(suggested_library), year=VALUES(year), updated_at=VALUES(updated_at)`,
			group.GroupKey, group.RawTitle, group.NormalizedTitle, group.MediaKind, group.SuggestedLibrary, group.Year, group.Status, group.DecisionSource, now, now)
	} else {
		_, err = tx.Exec(`INSERT INTO media_groups (group_key, raw_title, normalized_title, media_kind, suggested_library, year, status, decision_source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(group_key) DO UPDATE SET raw_title=excluded.raw_title, normalized_title=excluded.normalized_title, media_kind=excluded.media_kind, suggested_library=excluded.suggested_library, year=excluded.year, updated_at=excluded.updated_at`,
			group.GroupKey, group.RawTitle, group.NormalizedTitle, group.MediaKind, group.SuggestedLibrary, group.Year, group.Status, group.DecisionSource, now, now)
	}
	if err != nil {
		return 0, err
	}
	var groupID int64
	if err := tx.QueryRow(`SELECT id FROM media_groups WHERE group_key = ?`, group.GroupKey).Scan(&groupID); err != nil {
		return 0, err
	}
	for _, member := range members {
		if _, err := tx.Exec(`DELETE FROM media_group_members WHERE resource_id = ?`, member.ResourceID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`INSERT INTO media_group_members (group_id, resource_id, season, episode, confidence) VALUES (?, ?, ?, ?, ?)`,
			groupID, member.ResourceID, member.Season, member.Episode, member.Confidence); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return groupID, nil
}

// DeleteOrphanMediaGroups removes obsolete groups left after parser or grouping
// rules move every member to a new group. Dependent candidates/publications are
// removed by foreign keys; reusable aliases remain independent.
func (s *sqlStore) DeleteOrphanMediaGroups() (int64, error) {
	result, err := s.db.Exec(`DELETE FROM media_groups WHERE NOT EXISTS (
		SELECT 1 FROM media_group_members WHERE media_group_members.group_id = media_groups.id
	)`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *sqlStore) SetMediaGroupDecision(groupID int64, d MediaGroupDecision) error {
	result, err := s.db.Exec(`UPDATE media_groups SET status=?, decision_source=?, selected_source=?, selected_id=?, canonical_title=?, suggested_library=?, confidence=?, reason=?, updated_at=? WHERE id=?`,
		d.Status, d.DecisionSource, d.SelectedSource, d.SelectedID, d.CanonicalTitle, d.Library, d.Confidence, d.Reason, time.Now().Unix(), groupID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *sqlStore) ReplaceMediaCandidates(groupID int64, candidates []MediaCandidate) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM media_candidates WHERE group_id=?`, groupID); err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, candidate := range candidates {
		if _, err := tx.Exec(`INSERT INTO media_candidates (group_id, source, provider_id, title, original_title, year, media_kind, library, score, evidence_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, groupID, candidate.Source, candidate.ProviderID,
			candidate.Title, candidate.OriginalTitle, candidate.Year, candidate.MediaKind, candidate.Library,
			candidate.Score, candidate.EvidenceJSON, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqlStore) ListMediaGroupDetails(status string, limit, offset int) ([]MediaGroupDetail, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT ` + mediaGroupColumns + ` FROM media_groups`
	countQuery := `SELECT COUNT(*) FROM media_groups`
	args := []any{}
	if status != "" && status != "all" {
		query += ` WHERE status=?`
		countQuery += ` WHERE status=?`
		args = append(args, status)
	}
	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.Query(query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []MediaGroupDetail
	for rows.Next() {
		group, err := scanMediaGroup(rows)
		if err != nil {
			return nil, 0, err
		}
		detail, err := s.loadMediaGroupDetail(group)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, detail)
	}
	return out, total, rows.Err()
}

func (s *sqlStore) GetMediaGroupDetail(groupID int64) (MediaGroupDetail, error) {
	group, err := scanMediaGroup(s.db.QueryRow(`SELECT `+mediaGroupColumns+` FROM media_groups WHERE id=?`, groupID))
	if err != nil {
		return MediaGroupDetail{}, err
	}
	return s.loadMediaGroupDetail(group)
}

func (s *sqlStore) loadMediaGroupDetail(group MediaGroup) (MediaGroupDetail, error) {
	detail := MediaGroupDetail{Group: group, Members: []MediaGroupMember{}, Candidates: []MediaCandidate{}}
	rows, err := s.db.Query(`SELECT m.group_id, m.resource_id, m.season, m.episode, m.confidence, `+resourceCols+
		resourceJoin+` JOIN media_group_members m ON m.resource_id=r.id WHERE m.group_id=? ORDER BY m.season, m.episode, r.file_path`, group.ID)
	if err != nil {
		return detail, err
	}
	for rows.Next() {
		var member MediaGroupMember
		if err := rows.Scan(&member.GroupID, &member.ResourceID, &member.Season, &member.Episode, &member.Confidence,
			&member.Resource.ID, &member.Resource.SourceID, &member.Resource.Title, &member.Resource.Poster,
			&member.Resource.Overview, &member.Resource.SourceTitle, &member.Resource.SourceCategory, &member.Resource.Provider, &member.Resource.ShareURL,
			&member.Resource.SharePwd, &member.Resource.FilePath, &member.Resource.CreatedAt, &member.Resource.UpdatedAt); err != nil {
			rows.Close()
			return detail, err
		}
		detail.Members = append(detail.Members, member)
	}
	if err := rows.Close(); err != nil {
		return detail, err
	}
	cRows, err := s.db.Query(`SELECT id, group_id, source, provider_id, title, original_title, year, media_kind, library, score, evidence_json, created_at FROM media_candidates WHERE group_id=? ORDER BY score DESC, id`, group.ID)
	if err != nil {
		return detail, err
	}
	defer cRows.Close()
	for cRows.Next() {
		var c MediaCandidate
		if err := cRows.Scan(&c.ID, &c.GroupID, &c.Source, &c.ProviderID, &c.Title, &c.OriginalTitle, &c.Year, &c.MediaKind, &c.Library, &c.Score, &c.EvidenceJSON, &c.CreatedAt); err != nil {
			return detail, err
		}
		detail.Candidates = append(detail.Candidates, c)
	}
	return detail, cRows.Err()
}

func (s *sqlStore) GetResourceGroupStates() (map[int64]MediaGroupState, error) {
	rows, err := s.db.Query(`SELECT m.resource_id, g.id, g.status, g.suggested_library, g.canonical_title,
		g.selected_source, g.selected_id, g.media_kind, g.year
		FROM media_group_members m JOIN media_groups g ON g.id=m.group_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]MediaGroupState{}
	for rows.Next() {
		var resourceID int64
		var state MediaGroupState
		if err := rows.Scan(&resourceID, &state.GroupID, &state.Status, &state.Library, &state.CanonicalTitle,
			&state.SelectedSource, &state.SelectedID, &state.MediaKind, &state.Year); err != nil {
			return nil, err
		}
		out[resourceID] = state
	}
	return out, rows.Err()
}

func (s *sqlStore) RecordMediaPublication(p MediaPublication) error {
	if p.PublishedAt == 0 {
		p.PublishedAt = time.Now().Unix()
	}
	if p.Status == "" {
		p.Status = "published"
	}
	if p.Version == 0 {
		if err := s.db.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM media_publications WHERE group_id=?`, p.GroupID).Scan(&p.Version); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`INSERT INTO media_publications (group_id, version, status, output_path, metadata_hash, published_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.GroupID, p.Version, p.Status, p.OutputPath, p.MetadataHash, p.PublishedAt)
	if err != nil {
		return fmt.Errorf("记录媒体发布: %w", err)
	}
	_, _ = s.db.Exec(`UPDATE media_groups SET status='published', updated_at=? WHERE id=? AND status='verified'`, time.Now().Unix(), p.GroupID)
	return nil
}
