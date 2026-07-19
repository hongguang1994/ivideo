// Package store 是 SQLite 数据层：资源目录 + 缓存项状态机。
package store

import (
	"database/sql"
	_ "embed"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，无需 CGO
)

// 缓存项状态。
const (
	StatusUncached     = "uncached"     // 尚未转存
	StatusTransferring = "transferring" // 转存中
	StatusReady        = "ready"        // 已就绪，可播
	StatusFailed       = "failed"       // 转存失败
	StatusCleaned      = "cleaned"      // 已清理释放
)

//go:embed schema.sql
var schema string

// Store 封装数据库句柄。
type Store struct {
	db *sql.DB
}

// Resource 是一条收集来的分享资源。
type Resource struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Poster    string `json:"poster"`
	Overview  string `json:"overview"`
	Provider  string `json:"provider"`
	ShareURL  string `json:"shareUrl"`
	SharePwd  string `json:"sharePwd"`
	FilePath  string `json:"filePath"`
	CreatedAt int64  `json:"createdAt"`
}

// CacheItem 是某资源的转存缓存状态。
type CacheItem struct {
	ResourceID int64  `json:"resourceId"`
	Backend    string `json:"backend"`
	Status     string `json:"status"`
	CachePath  string `json:"cachePath"`
	DirectURL  string `json:"-"` // 不直接暴露给前端
	Size       int64  `json:"size"`
	LastAccess int64  `json:"lastAccess"`
	Error      string `json:"error"`
	UpdatedAt  int64  `json:"updatedAt"`
}

// Open 打开（或创建）数据库并建表。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// ---- 资源目录 ----

// AddResource 新增一条资源，返回其 ID。
func (s *Store) AddResource(r Resource) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO resources (title, poster, overview, provider, share_url, share_pwd, file_path, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Title, r.Poster, r.Overview, r.Provider, r.ShareURL, r.SharePwd, r.FilePath, time.Now().Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListResources 返回全部资源。
func (s *Store) ListResources() ([]Resource, error) {
	rows, err := s.db.Query(
		`SELECT id, title, poster, overview, provider, share_url, share_pwd, file_path, created_at
		 FROM resources ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Resource
	for rows.Next() {
		var r Resource
		if err := rows.Scan(&r.ID, &r.Title, &r.Poster, &r.Overview, &r.Provider,
			&r.ShareURL, &r.SharePwd, &r.FilePath, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetResource 按 ID 取资源。
func (s *Store) GetResource(id int64) (Resource, error) {
	var r Resource
	err := s.db.QueryRow(
		`SELECT id, title, poster, overview, provider, share_url, share_pwd, file_path, created_at
		 FROM resources WHERE id = ?`, id).
		Scan(&r.ID, &r.Title, &r.Poster, &r.Overview, &r.Provider,
			&r.ShareURL, &r.SharePwd, &r.FilePath, &r.CreatedAt)
	return r, err
}

// CountResources 返回资源条数（用于判断是否需要 seed）。
func (s *Store) CountResources() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM resources`).Scan(&n)
	return n, err
}

// ---- 缓存项状态机 ----

// GetCacheItem 取缓存项；不存在时返回 status=uncached 的零值。
func (s *Store) GetCacheItem(resourceID int64) (CacheItem, error) {
	var c CacheItem
	err := s.db.QueryRow(
		`SELECT resource_id, backend, status, cache_path, direct_url, size, last_access,
		        COALESCE(error, ''), updated_at
		 FROM cache_items WHERE resource_id = ?`, resourceID).
		Scan(&c.ResourceID, &c.Backend, &c.Status, &c.CachePath, &c.DirectURL,
			&c.Size, &c.LastAccess, &c.Error, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return CacheItem{ResourceID: resourceID, Status: StatusUncached}, nil
	}
	return c, err
}

// SetTransferring 标记为转存中。
func (s *Store) SetTransferring(resourceID int64, backend string) error {
	return s.upsertStatus(resourceID, backend, StatusTransferring, "")
}

// SetFailed 标记为失败并记录原因。
func (s *Store) SetFailed(resourceID int64, backend, errMsg string) error {
	return s.upsertStatus(resourceID, backend, StatusFailed, errMsg)
}

// SetReady 标记为就绪，写入路径/直链/大小，并刷新访问时间。
func (s *Store) SetReady(resourceID int64, backend, cachePath, directURL string, size int64) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`INSERT INTO cache_items (resource_id, backend, status, cache_path, direct_url, size, last_access, error, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)
		 ON CONFLICT(resource_id) DO UPDATE SET
		    backend=excluded.backend, status=excluded.status, cache_path=excluded.cache_path,
		    direct_url=excluded.direct_url, size=excluded.size, last_access=excluded.last_access,
		    error='', updated_at=excluded.updated_at`,
		resourceID, backend, StatusReady, cachePath, directURL, size, now, now)
	return err
}

// TouchAccess 刷新最后访问时间（LRU 用）。
func (s *Store) TouchAccess(resourceID int64) error {
	_, err := s.db.Exec(`UPDATE cache_items SET last_access = ? WHERE resource_id = ?`,
		time.Now().Unix(), resourceID)
	return err
}

// MarkCleaned 标记为已清理，清空路径/直链/大小。
func (s *Store) MarkCleaned(resourceID int64) error {
	_, err := s.db.Exec(
		`UPDATE cache_items SET status = ?, cache_path = '', direct_url = '', size = 0, updated_at = ?
		 WHERE resource_id = ?`, StatusCleaned, time.Now().Unix(), resourceID)
	return err
}

// ListReady 返回全部就绪缓存项，按最后访问时间升序（最久未看在前，供 LRU 淘汰）。
func (s *Store) ListReady() ([]CacheItem, error) {
	rows, err := s.db.Query(
		`SELECT resource_id, backend, status, cache_path, direct_url, size, last_access,
		        COALESCE(error, ''), updated_at
		 FROM cache_items WHERE status = ? ORDER BY last_access ASC`, StatusReady)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CacheItem
	for rows.Next() {
		var c CacheItem
		if err := rows.Scan(&c.ResourceID, &c.Backend, &c.Status, &c.CachePath, &c.DirectURL,
			&c.Size, &c.LastAccess, &c.Error, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- 网盘凭据 ----

// Credential 是一个网盘的凭据。
type Credential struct {
	Provider  string `json:"provider"`
	Token     string `json:"token"`
	Extra     string `json:"extra"`
	UpdatedAt int64  `json:"updatedAt"`
}

// GetCredential 取某网盘凭据；不存在返回零值 + found=false。
func (s *Store) GetCredential(provider string) (Credential, bool, error) {
	var c Credential
	err := s.db.QueryRow(
		`SELECT provider, token, extra, updated_at FROM credentials WHERE provider = ?`, provider).
		Scan(&c.Provider, &c.Token, &c.Extra, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return Credential{Provider: provider}, false, nil
	}
	return c, err == nil, err
}

// SetCredential 写入/更新某网盘凭据。
func (s *Store) SetCredential(provider, token, extra string) error {
	_, err := s.db.Exec(
		`INSERT INTO credentials (provider, token, extra, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(provider) DO UPDATE SET token=excluded.token, extra=excluded.extra, updated_at=excluded.updated_at`,
		provider, token, extra, time.Now().Unix())
	return err
}

// SetCredentialToken 只更新 token（用于 token 轮换时保存新值，不动 extra）。
func (s *Store) SetCredentialToken(provider, token string) error {
	_, err := s.db.Exec(
		`INSERT INTO credentials (provider, token, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(provider) DO UPDATE SET token=excluded.token, updated_at=excluded.updated_at`,
		provider, token, time.Now().Unix())
	return err
}

// ListCredentialProviders 返回已配置凭据的网盘列表（不含 token 值，仅状态）。
func (s *Store) ListCredentialProviders() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT provider, token != '' FROM credentials`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		var ok bool
		if err := rows.Scan(&p, &ok); err != nil {
			return nil, err
		}
		out[p] = ok
	}
	return out, rows.Err()
}

// upsertStatus 是只改状态/错误的通用 upsert。
func (s *Store) upsertStatus(resourceID int64, backend, status, errMsg string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`INSERT INTO cache_items (resource_id, backend, status, size, last_access, error, updated_at)
		 VALUES (?, ?, ?, 0, 0, ?, ?)
		 ON CONFLICT(resource_id) DO UPDATE SET
		    backend=excluded.backend, status=excluded.status, error=excluded.error, updated_at=excluded.updated_at`,
		resourceID, backend, status, errMsg, now)
	return err
}
