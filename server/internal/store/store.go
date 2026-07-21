// Package store 是数据层：定义 Store 接口 + 具体实现（当前 SQLite）。
// 切换数据库（如 MySQL）时新增一个实现即可，上层只依赖 Store 接口。
package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // MySQL 驱动
	_ "modernc.org/sqlite"             // 纯 Go SQLite 驱动，无需 CGO
)

// 缓存项状态。
const (
	StatusUncached     = "uncached"     // 尚未转存
	StatusTransferring = "transferring" // 转存中
	StatusReady        = "ready"        // 已就绪，可播
	StatusFailed       = "failed"       // 转存失败
	StatusCleaned      = "cleaned"      // 已清理释放
)

// Store 是数据层接口。上层（handlers/app/cache/strm）只依赖此接口，
// 不关心底层是 SQLite 还是 MySQL —— 换库只需新增一个实现。
type Store interface {
	Close() error

	// 资源目录
	AddResource(r Resource) (int64, error)
	ListResources() ([]Resource, error)
	GetResource(id int64) (Resource, error)
	CountResources() (int, error)

	// 缓存项状态机
	GetCacheItem(resourceID int64) (CacheItem, error)
	SetTransferring(resourceID int64, backend string) error
	SetFailed(resourceID int64, backend, errMsg string) error
	SetReady(resourceID int64, backend, cachePath, directURL string, size int64) error
	TouchAccess(resourceID int64) error
	MarkCleaned(resourceID int64) error
	ListReady() ([]CacheItem, error)

	// 网盘凭据
	GetCredential(provider string) (Credential, bool, error)
	SetCredential(provider, token, extra string) error
	SetCredentialToken(provider, token string) error
	ListCredentialProviders() (map[string]bool, error)

	// 分享库（收藏的网盘分享链接）
	AddShare(s Share) (int64, error)
	ListShares() ([]Share, error)
	GetShare(id int64) (Share, error)
	UpdateShare(s Share) error
	DeleteShare(id int64) error
}

// sqlStore 是 Store 的 database/sql 实现；方言差异(建表 DDL、upsert)由 dialect 收口。
type sqlStore struct {
	db *sql.DB
	d  dialect
}

// 编译期断言：sqlStore 必须完整实现 Store 接口。
var _ Store = (*sqlStore)(nil)

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

// Open 按驱动(sqlite / mysql)打开数据库并建表，返回 Store 接口。
//   - sqlite:dsn 为文件路径
//   - mysql :dsn 为标准 DSN，如 user:pass@tcp(host:3306)/ivideo?charset=utf8mb4
func Open(driver, dsn string) (Store, error) {
	d, err := dialectFor(driver)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(d.driver, dsn)
	if err != nil {
		return nil, err
	}
	// 等数据库就绪：MySQL 容器刚起时可能还没接受连接，重试避免启动即崩溃重启。
	if err := pingWithRetry(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := execSchema(db, d.schema); err != nil {
		db.Close()
		return nil, err
	}
	return &sqlStore{db: db, d: d}, nil
}

// pingWithRetry 最多重试 ~60s 等数据库可连（sqlite 通常一次就成）。
func pingWithRetry(db *sql.DB) error {
	var err error
	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("数据库连接超时: %w", err)
}

// execSchema 按 ; 拆分逐条执行建表语句（MySQL 驱动默认不允许一次多语句）。
func execSchema(db *sql.DB, schema string) error {
	for _, stmt := range strings.Split(schema, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("建表失败: %w", err)
		}
	}
	return nil
}

// Close 关闭数据库。
func (s *sqlStore) Close() error { return s.db.Close() }

// ---- 资源目录 ----

// AddResource 新增一条资源，返回其 ID。
func (s *sqlStore) AddResource(r Resource) (int64, error) {
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
func (s *sqlStore) ListResources() ([]Resource, error) {
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
func (s *sqlStore) GetResource(id int64) (Resource, error) {
	var r Resource
	err := s.db.QueryRow(
		`SELECT id, title, poster, overview, provider, share_url, share_pwd, file_path, created_at
		 FROM resources WHERE id = ?`, id).
		Scan(&r.ID, &r.Title, &r.Poster, &r.Overview, &r.Provider,
			&r.ShareURL, &r.SharePwd, &r.FilePath, &r.CreatedAt)
	return r, err
}

// CountResources 返回资源条数（用于判断是否需要 seed）。
func (s *sqlStore) CountResources() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM resources`).Scan(&n)
	return n, err
}

// ---- 缓存项状态机 ----

// GetCacheItem 取缓存项；不存在时返回 status=uncached 的零值。
func (s *sqlStore) GetCacheItem(resourceID int64) (CacheItem, error) {
	var c CacheItem
	err := s.db.QueryRow(
		`SELECT resource_id, backend, status, COALESCE(cache_path,''), COALESCE(direct_url,''),
		        size, last_access, COALESCE(error, ''), updated_at
		 FROM cache_items WHERE resource_id = ?`, resourceID).
		Scan(&c.ResourceID, &c.Backend, &c.Status, &c.CachePath, &c.DirectURL,
			&c.Size, &c.LastAccess, &c.Error, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return CacheItem{ResourceID: resourceID, Status: StatusUncached}, nil
	}
	return c, err
}

// SetTransferring 标记为转存中。
func (s *sqlStore) SetTransferring(resourceID int64, backend string) error {
	return s.upsertStatus(resourceID, backend, StatusTransferring, "")
}

// SetFailed 标记为失败并记录原因。
func (s *sqlStore) SetFailed(resourceID int64, backend, errMsg string) error {
	return s.upsertStatus(resourceID, backend, StatusFailed, errMsg)
}

// SetReady 标记为就绪，写入路径/直链/大小，并刷新访问时间。
func (s *sqlStore) SetReady(resourceID int64, backend, cachePath, directURL string, size int64) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(s.d.upsertReady,
		resourceID, backend, StatusReady, cachePath, directURL, size, now, now)
	return err
}

// TouchAccess 刷新最后访问时间（LRU 用）。
func (s *sqlStore) TouchAccess(resourceID int64) error {
	_, err := s.db.Exec(`UPDATE cache_items SET last_access = ? WHERE resource_id = ?`,
		time.Now().Unix(), resourceID)
	return err
}

// MarkCleaned 标记为已清理，清空路径/直链/大小。
func (s *sqlStore) MarkCleaned(resourceID int64) error {
	_, err := s.db.Exec(
		`UPDATE cache_items SET status = ?, cache_path = '', direct_url = '', size = 0, updated_at = ?
		 WHERE resource_id = ?`, StatusCleaned, time.Now().Unix(), resourceID)
	return err
}

// ListReady 返回全部就绪缓存项，按最后访问时间升序（最久未看在前，供 LRU 淘汰）。
func (s *sqlStore) ListReady() ([]CacheItem, error) {
	rows, err := s.db.Query(
		`SELECT resource_id, backend, status, COALESCE(cache_path,''), COALESCE(direct_url,''),
		        size, last_access, COALESCE(error, ''), updated_at
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

// Share 是收藏的一个网盘分享（整份分享，区别于 Resource 的单个文件）。
type Share struct {
	ID            int64  `json:"id"`
	Provider      string `json:"provider"`      // aliyun / 115 / quark / ...
	ShareURL      string `json:"shareUrl"`      // 分享链接
	SharePwd      string `json:"sharePwd"`      // 提取码（可选）
	ShareID       string `json:"shareId"`       // 从链接提取的分享 ID（可选）
	Title         string `json:"title"`         // 名称/标题
	Remark        string `json:"remark"`        // 备注
	Category      string `json:"category"`      // 分类
	Status        string `json:"status"`        // unknown / valid / invalid
	LastCheckedAt int64  `json:"lastCheckedAt"` // 上次校验有效性（unix）
	FileCount     int    `json:"fileCount"`     // 分享内条目数（浏览后缓存）
	TotalSize     int64  `json:"totalSize"`     // 总大小（字节）
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
}

// GetCredential 取某网盘凭据；不存在返回零值 + found=false。
func (s *sqlStore) GetCredential(provider string) (Credential, bool, error) {
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
func (s *sqlStore) SetCredential(provider, token, extra string) error {
	_, err := s.db.Exec(s.d.upsertCredential,
		provider, token, extra, time.Now().Unix())
	return err
}

// SetCredentialToken 只更新 token（用于 token 轮换时保存新值，不动 extra）。
func (s *sqlStore) SetCredentialToken(provider, token string) error {
	_, err := s.db.Exec(s.d.upsertCredToken,
		provider, token, time.Now().Unix())
	return err
}

// ListCredentialProviders 返回已配置凭据的网盘列表（不含 token 值，仅状态）。
func (s *sqlStore) ListCredentialProviders() (map[string]bool, error) {
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
func (s *sqlStore) upsertStatus(resourceID int64, backend, status, errMsg string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(s.d.upsertStatus,
		resourceID, backend, status, errMsg, now)
	return err
}
