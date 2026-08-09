package store

import (
	"database/sql"
	"time"
)

// shareCols 是收藏来源查询列（COALESCE 掉可空列，避免 NULL 扫描报错）。
const shareCols = `id, provider, share_url, COALESCE(share_pwd,''), COALESCE(share_id,''),
	COALESCE(title,''), COALESCE(remark,''), COALESCE(category,''),
	status, last_checked_at, file_count, total_size, created_at, updated_at`

// rowScanner 兼容 *sql.Row 与 *sql.Rows。
type rowScanner interface{ Scan(dest ...any) error }

func scanShare(sc rowScanner) (Share, error) {
	var sh Share
	err := sc.Scan(&sh.ID, &sh.Provider, &sh.ShareURL, &sh.SharePwd, &sh.ShareID,
		&sh.Title, &sh.Remark, &sh.Category, &sh.Status, &sh.LastCheckedAt,
		&sh.FileCount, &sh.TotalSize, &sh.CreatedAt, &sh.UpdatedAt)
	return sh, err
}

// AddShare 收藏一个分享；若来源已被资源使用，仅恢复其收藏标记，不复制来源数据。
func (s *sqlStore) AddShare(sh Share) (int64, error) {
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := ensureShareSource(tx, sh, true, now)
	if err != nil {
		return 0, err
	}
	if err := s.syncCanonicalLegacySource(tx, id, now); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// SyncShares 批量写入外部发现的分享。已存在的来源只合并新元数据，并恢复收藏标记。
// 使用单事务，避免同步大型资源源时产生大量短事务。
func (s *sqlStore) SyncShares(shares []Share) (added, existing int, err error) {
	if len(shares) == 0 {
		return 0, 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	seen := make(map[string]struct{}, len(shares))
	for _, sh := range shares {
		key := shareSourceKey(sh.Provider, sh.ShareURL)
		if key == "\x00" || key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		var oldID int64
		lookupErr := tx.QueryRow(`SELECT id FROM share_sources WHERE source_key = ?`, key).Scan(&oldID)
		if lookupErr == nil {
			existing++
		} else if lookupErr != sql.ErrNoRows {
			return 0, 0, lookupErr
		}
		sourceID, err := ensureShareSource(tx, sh, true, now)
		if err != nil {
			return 0, 0, err
		}
		if err := s.syncCanonicalLegacySource(tx, sourceID, now); err != nil {
			return 0, 0, err
		}
		if lookupErr == sql.ErrNoRows {
			added++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return added, existing, nil
}

// ListShares 返回全部收藏的分享，按收藏时间倒序。
func (s *sqlStore) ListShares() ([]Share, error) {
	rows, err := s.db.Query(`SELECT ` + shareCols + ` FROM share_sources WHERE is_bookmarked = 1 ORDER BY created_at DESC`)
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

// GetShare 按 ID 取分享；不存在返回 sql.ErrNoRows。
func (s *sqlStore) GetShare(id int64) (Share, error) {
	return scanShare(s.db.QueryRow(`SELECT `+shareCols+` FROM share_sources WHERE id = ? AND is_bookmarked = 1`, id))
}

// UpdateShare 更新可编辑字段（标题/备注/分类/状态/提取码/校验时间/统计）。
func (s *sqlStore) UpdateShare(sh Share) error {
	_, err := s.db.Exec(
		`UPDATE share_sources SET title=?, remark=?, category=?, status=?, share_pwd=?,
			last_checked_at=?, file_count=?, total_size=?, updated_at=? WHERE id=?`,
		sh.Title, sh.Remark, sh.Category, sh.Status, sh.SharePwd,
		sh.LastCheckedAt, sh.FileCount, sh.TotalSize, time.Now().Unix(), sh.ID)
	return err
}

// DeleteShare 取消收藏，但保留仍被资源引用的来源。
func (s *sqlStore) DeleteShare(id int64) error {
	_, err := s.db.Exec(`UPDATE share_sources SET is_bookmarked = 0, updated_at = ? WHERE id = ?`, time.Now().Unix(), id)
	return err
}

func ensureShareSource(tx *sql.Tx, sh Share, bookmarked bool, now int64) (int64, error) {
	status := sh.Status
	if status == "" {
		status = "unknown"
	}
	key := shareSourceKey(sh.Provider, sh.ShareURL)
	res, err := tx.Exec(
		`INSERT INTO share_sources (provider, share_url, share_pwd, share_id, source_key, is_bookmarked,
			title, remark, category, status, last_checked_at, file_count, total_size, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sh.Provider, sh.ShareURL, sh.SharePwd, sh.ShareID, key, bookmarked,
		sh.Title, sh.Remark, sh.Category, status, sh.LastCheckedAt, sh.FileCount, sh.TotalSize, now, now,
	)
	if err == nil {
		return res.LastInsertId()
	}
	if !isDuplicateKey(err) {
		return 0, err
	}
	if bookmarked {
		if _, err := tx.Exec(
			`UPDATE share_sources SET is_bookmarked=1, share_pwd=CASE WHEN ? <> '' THEN ? ELSE share_pwd END,
				share_id=CASE WHEN ? <> '' THEN ? ELSE share_id END, title=CASE WHEN ? <> '' THEN ? ELSE title END,
				remark=CASE WHEN ? <> '' THEN ? ELSE remark END, category=CASE WHEN ? <> '' THEN ? ELSE category END,
				updated_at=? WHERE source_key=?`,
			sh.SharePwd, sh.SharePwd, sh.ShareID, sh.ShareID, sh.Title, sh.Title,
			sh.Remark, sh.Remark, sh.Category, sh.Category, now, key); err != nil {
			return 0, err
		}
	}
	var id int64
	err = tx.QueryRow(`SELECT id FROM share_sources WHERE source_key = ?`, key).Scan(&id)
	return id, err
}
