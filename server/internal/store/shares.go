package store

import (
	"database/sql"
	"time"
)

// shares is the single source of truth for a public link. Provenance belongs
// in share_observations; imported files reference shares.id directly.
const shareCols = `id, provider, share_url, share_pwd, share_id, title, remark, category,
	status, last_checked_at, file_count, total_size, created_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanShare(sc rowScanner) (Share, error) {
	var sh Share
	err := sc.Scan(&sh.ID, &sh.Provider, &sh.ShareURL, &sh.SharePwd, &sh.ShareID,
		&sh.Title, &sh.Remark, &sh.Category, &sh.Status, &sh.LastCheckedAt,
		&sh.FileCount, &sh.TotalSize, &sh.CreatedAt, &sh.UpdatedAt)
	return sh, err
}

func (s *sqlStore) AddShare(sh Share) (int64, error) {
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, _, err := s.ensureShare(tx, sh, true, now)
	if err != nil {
		return 0, err
	}
	if err := s.recordManualObservation(tx, id, sh, now); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *sqlStore) SyncShares(items []Share) (added, existing int, err error) {
	if len(items) == 0 {
		return 0, 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	seen := make(map[string]struct{}, len(items))
	for _, sh := range items {
		key := shareSourceKey(sh.Provider, sh.ShareURL)
		if key == "" || key == "\x00" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		_, created, ensureErr := s.ensureShare(tx, sh, true, now)
		if ensureErr != nil {
			return 0, 0, ensureErr
		}
		if created {
			added++
		} else {
			existing++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return added, existing, nil
}

func (s *sqlStore) ListShares() ([]Share, error) {
	rows, err := s.db.Query(`SELECT ` + shareCols + ` FROM shares WHERE is_bookmarked=1 ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Share
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func (s *sqlStore) GetShare(id int64) (Share, error) {
	return scanShare(s.db.QueryRow(`SELECT `+shareCols+` FROM shares WHERE id=? AND is_bookmarked=1`, id))
}

func (s *sqlStore) UpdateShare(sh Share) error {
	_, err := s.db.Exec(`UPDATE shares SET title=?, remark=?, category=?, status=?, share_pwd=?,
		last_checked_at=?, file_count=?, total_size=?, updated_at=?, last_seen_at=? WHERE id=?`,
		sh.Title, sh.Remark, sh.Category, sh.Status, sh.SharePwd, sh.LastCheckedAt, sh.FileCount,
		sh.TotalSize, time.Now().Unix(), time.Now().Unix(), sh.ID)
	return err
}

// DeleteShare removes a user bookmark but preserves a link when resources or
// other observations still reference it.
func (s *sqlStore) DeleteShare(id int64) error {
	_, err := s.db.Exec(`UPDATE shares SET is_bookmarked=0, updated_at=? WHERE id=?`, time.Now().Unix(), id)
	return err
}

func (s *sqlStore) ensureShare(tx *sql.Tx, sh Share, bookmarked bool, now int64) (int64, bool, error) {
	key := shareSourceKey(sh.Provider, sh.ShareURL)
	if key == "" || key == "\x00" {
		return 0, false, sql.ErrNoRows
	}
	status := sh.Status
	if status == "" {
		status = "unknown"
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM shares WHERE canonical_key=?`, key).Scan(&id)
	if err == nil {
		_, err = tx.Exec(`UPDATE shares SET is_bookmarked=CASE WHEN ? THEN 1 ELSE is_bookmarked END,
			share_pwd=CASE WHEN ?<>'' THEN ? ELSE share_pwd END, share_id=CASE WHEN ?<>'' THEN ? ELSE share_id END,
			title=CASE WHEN ?<>'' THEN ? ELSE title END, remark=CASE WHEN ?<>'' THEN ? ELSE remark END,
			category=CASE WHEN ?<>'' THEN ? ELSE category END, last_seen_at=?, updated_at=? WHERE id=?`,
			bookmarked, sh.SharePwd, sh.SharePwd, sh.ShareID, sh.ShareID, sh.Title, sh.Title, sh.Remark, sh.Remark,
			sh.Category, sh.Category, now, now, id)
		return id, false, err
	}
	if err != sql.ErrNoRows {
		return 0, false, err
	}
	res, err := tx.Exec(`INSERT INTO shares (provider, share_url, share_pwd, share_id, canonical_key, is_bookmarked,
		title, remark, category, status, last_checked_at, file_count, total_size, first_seen_at, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sh.Provider, sh.ShareURL, sh.SharePwd, sh.ShareID, key, bookmarked, sh.Title, sh.Remark, sh.Category, status,
		sh.LastCheckedAt, sh.FileCount, sh.TotalSize, now, now, now, now)
	if err != nil {
		return 0, false, err
	}
	id, err = res.LastInsertId()
	return id, true, err
}

func (s *sqlStore) recordManualObservation(tx *sql.Tx, shareID int64, sh Share, now int64) error {
	sourceID, err := s.ensureDiscoverySource(tx, "manual", "bookmarks", "手动收藏", now)
	if err != nil {
		return err
	}
	return s.upsertCanonicalObservation(tx, shareID, sourceID, "share:"+shareSourceKey(sh.Provider, sh.ShareURL), GitHubObservedShare{
		Provider: sh.Provider, ShareURL: sh.ShareURL, SharePwd: sh.SharePwd, Title: sh.Title, ResourceType: sh.Category,
	}, now)
}
