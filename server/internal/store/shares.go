package store

import "time"

// shareCols 是 shares 表查询列（COALESCE 掉可空列，避免 NULL 扫描报错）。
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

// AddShare 收藏一个分享，返回其 ID。
// (provider, share_url) 有唯一约束，重复收藏会报错（由上层提示"已存在"）。
func (s *sqlStore) AddShare(sh Share) (int64, error) {
	now := time.Now().Unix()
	status := sh.Status
	if status == "" {
		status = "unknown"
	}
	res, err := s.db.Exec(
		`INSERT INTO shares (provider, share_url, share_pwd, share_id, title, remark, category,
			status, last_checked_at, file_count, total_size, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sh.Provider, sh.ShareURL, sh.SharePwd, sh.ShareID, sh.Title, sh.Remark, sh.Category,
		status, sh.LastCheckedAt, sh.FileCount, sh.TotalSize, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListShares 返回全部收藏的分享，按收藏时间倒序。
func (s *sqlStore) ListShares() ([]Share, error) {
	rows, err := s.db.Query(`SELECT ` + shareCols + ` FROM shares ORDER BY created_at DESC`)
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
	return scanShare(s.db.QueryRow(`SELECT `+shareCols+` FROM shares WHERE id = ?`, id))
}

// UpdateShare 更新可编辑字段（标题/备注/分类/状态/提取码/校验时间/统计）。
func (s *sqlStore) UpdateShare(sh Share) error {
	_, err := s.db.Exec(
		`UPDATE shares SET title=?, remark=?, category=?, status=?, share_pwd=?,
			last_checked_at=?, file_count=?, total_size=?, updated_at=? WHERE id=?`,
		sh.Title, sh.Remark, sh.Category, sh.Status, sh.SharePwd,
		sh.LastCheckedAt, sh.FileCount, sh.TotalSize, time.Now().Unix(), sh.ID)
	return err
}

// DeleteShare 删除一个收藏的分享。
func (s *sqlStore) DeleteShare(id int64) error {
	_, err := s.db.Exec(`DELETE FROM shares WHERE id = ?`, id)
	return err
}
