// Package store 定义按业务域拆分的仓储接口，并提供 MySQL/SQLite 的 database/sql 实现。
package store

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path"
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

// ErrResourceExists 表示同一分享中的同一文件已经入库。
var ErrResourceExists = errors.New("资源已存在")

// Store is the compatibility aggregate used by the composition root. Business
// modules depend on the smaller repository interfaces in repositories.go.
type Store interface {
	ResourceRepository
	CacheRepository
	CredentialRepository
	SettingsRepository
	MediaRepository
	JobRepository
	ShareRepository
	Close() error
}

// sqlStore 是 Store 的 database/sql 实现；方言差异(建表 DDL、upsert)由 dialect 收口。
type sqlStore struct {
	db *sql.DB
	d  dialect
}

// 编译期断言：sqlStore 必须完整实现 Store 接口。
var _ Store = (*sqlStore)(nil)
var _ ResourceRepository = (*sqlStore)(nil)
var _ CacheRepository = (*sqlStore)(nil)
var _ CredentialRepository = (*sqlStore)(nil)
var _ SettingsRepository = (*sqlStore)(nil)
var _ MediaRepository = (*sqlStore)(nil)
var _ JobRepository = (*sqlStore)(nil)
var _ ShareRepository = (*sqlStore)(nil)

// Resource 是一条收集来的分享资源。
type Resource struct {
	ID             int64  `json:"id"`
	SourceID       int64  `json:"sourceId"`
	Title          string `json:"title"`
	SourceTitle    string `json:"sourceTitle,omitempty"`
	SourceCategory string `json:"sourceCategory,omitempty"`
	Poster         string `json:"poster"`
	Overview       string `json:"overview"`
	Provider       string `json:"provider"`
	ShareURL       string `json:"shareUrl"`
	SharePwd       string `json:"sharePwd"`
	FilePath       string `json:"filePath"`
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
	ResourceKey    string `json:"-"`
}

// ResourceMediaDecision 是作品组决定在单个资源上的只读投影。
type ResourceMediaDecision struct {
	ResourceID int64  `json:"resourceId"`
	Source     string `json:"source"`
	ProviderID string `json:"providerId"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	Confidence int    `json:"confidence"`
	Status     string `json:"status"`
	Reason     string `json:"reason"`
	UpdatedAt  int64  `json:"updatedAt"`
}

// ContentAnalysis 保存从视频内容抽取的轻量证据。
type ContentAnalysis struct {
	ResourceID      int64  `json:"resourceId"`
	DurationSeconds int    `json:"durationSeconds"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	VideoCodec      string `json:"videoCodec"`
	AudioLanguages  string `json:"audioLanguages"`
	FrameSignature  string `json:"frameSignature"`
	OCRText         string `json:"ocrText"`
	Status          string `json:"status"`
	Error           string `json:"error"`
	AnalyzedAt      int64  `json:"analyzedAt"`
}

type MediaGroup struct {
	ID               int64  `json:"id"`
	GroupKey         string `json:"groupKey"`
	RawTitle         string `json:"rawTitle"`
	NormalizedTitle  string `json:"normalizedTitle"`
	MediaKind        string `json:"mediaKind"`
	SuggestedLibrary string `json:"suggestedLibrary"`
	Year             int    `json:"year"`
	Status           string `json:"status"`
	DecisionSource   string `json:"decisionSource"`
	SelectedSource   string `json:"selectedSource"`
	SelectedID       string `json:"selectedId"`
	CanonicalTitle   string `json:"canonicalTitle"`
	Confidence       int    `json:"confidence"`
	Reason           string `json:"reason"`
	CreatedAt        int64  `json:"createdAt"`
	UpdatedAt        int64  `json:"updatedAt"`
}

type MediaGroupMember struct {
	GroupID    int64    `json:"groupId"`
	ResourceID int64    `json:"resourceId"`
	Season     int      `json:"season"`
	Episode    int      `json:"episode"`
	Confidence int      `json:"confidence"`
	Resource   Resource `json:"resource"`
}

type MediaCandidate struct {
	ID            int64  `json:"id"`
	GroupID       int64  `json:"groupId"`
	Source        string `json:"source"`
	ProviderID    string `json:"providerId"`
	Title         string `json:"title"`
	OriginalTitle string `json:"originalTitle"`
	Year          int    `json:"year"`
	MediaKind     string `json:"mediaKind"`
	Library       string `json:"library"`
	Score         int    `json:"score"`
	EvidenceJSON  string `json:"evidenceJson"`
	CreatedAt     int64  `json:"createdAt"`
}

type MediaGroupDecision struct {
	Status         string
	DecisionSource string
	SelectedSource string
	SelectedID     string
	CanonicalTitle string
	Library        string
	Confidence     int
	Reason         string
}

type MediaGroupDetail struct {
	Group      MediaGroup         `json:"group"`
	Members    []MediaGroupMember `json:"members"`
	Candidates []MediaCandidate   `json:"candidates"`
}

type MediaGroupState struct {
	GroupID        int64
	Status         string
	Library        string
	CanonicalTitle string
	SelectedSource string
	SelectedID     string
	MediaKind      string
	Year           int
}

type MediaPublication struct {
	ID           int64  `json:"id"`
	GroupID      int64  `json:"groupId"`
	Version      int    `json:"version"`
	Status       string `json:"status"`
	OutputPath   string `json:"outputPath"`
	MetadataHash string `json:"metadataHash"`
	PublishedAt  int64  `json:"publishedAt"`
}

type MediaPathAnalysis struct {
	ResourceID      int64  `json:"resourceId"`
	AnalyzerVersion string `json:"analyzerVersion"`
	PathHash        string `json:"pathHash"`
	AnalysisJSON    string `json:"analysisJson"`
	AnalyzedAt      int64  `json:"analyzedAt"`
}

type MediaTitleCandidate struct {
	ID              int64  `json:"id"`
	ResourceID      int64  `json:"resourceId"`
	Title           string `json:"title"`
	NormalizedTitle string `json:"normalizedTitle"`
	Year            int    `json:"year"`
	Source          string `json:"source"`
	Weight          int    `json:"weight"`
	AutoEligible    bool   `json:"autoEligible"`
	CreatedAt       int64  `json:"createdAt"`
}

type MediaVerification struct {
	ID              int64  `json:"id"`
	GroupID         int64  `json:"groupId"`
	ResourceID      int64  `json:"resourceId"`
	Verifier        string `json:"verifier"`
	VerifierVersion string `json:"verifierVersion"`
	Status          string `json:"status"`
	ScoreDelta      int    `json:"scoreDelta"`
	Reason          string `json:"reason"`
	EvidenceJSON    string `json:"evidenceJson"`
	CreatedAt       int64  `json:"createdAt"`
}

type MediaProcessingJob struct {
	ID             int64  `json:"id"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	TotalCount     int    `json:"totalCount"`
	ProcessedCount int    `json:"processedCount"`
	FailedCount    int    `json:"failedCount"`
	Error          string `json:"error"`
	StartedAt      int64  `json:"startedAt"`
	FinishedAt     int64  `json:"finishedAt"`
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
}

type MediaProcessingStep struct {
	ID         int64  `json:"id"`
	JobID      int64  `json:"jobId"`
	GroupID    int64  `json:"groupId"`
	ResourceID int64  `json:"resourceId"`
	Stage      string `json:"stage"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
}

type MediaPublicationArtifact struct {
	ID           int64  `json:"id"`
	GroupID      int64  `json:"groupId"`
	ArtifactType string `json:"artifactType"`
	Path         string `json:"path"`
	ContentHash  string `json:"contentHash"`
	Status       string `json:"status"`
	Error        string `json:"error"`
	UpdatedAt    int64  `json:"updatedAt"`
}

type MediaTag struct {
	ID         int64  `json:"id"`
	GroupID    int64  `json:"groupId"`
	Name       string `json:"name"`
	Normalized string `json:"normalized"`
	Source     string `json:"source"`
	Confidence int    `json:"confidence"`
	CreatedAt  int64  `json:"createdAt"`
}

// MediaAlias stores a reusable mapping from a path/source title to a provider
// identity. Manual confirmations take precedence over automatically learned aliases.
type MediaAlias struct {
	ID              int64  `json:"id"`
	Alias           string `json:"alias"`
	NormalizedAlias string `json:"normalizedAlias"`
	CanonicalTitle  string `json:"canonicalTitle"`
	MediaKind       string `json:"mediaKind"`
	Source          string `json:"source"`
	ProviderID      string `json:"providerId"`
	Year            int    `json:"year"`
	Confidence      int    `json:"confidence"`
	HitCount        int    `json:"hitCount"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
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
	if d.driver == "sqlite" {
		// foreign_keys 是 SQLite 的连接级开关；单连接避免连接池绕过约束。
		db.SetMaxOpenConns(1)
	}
	// 等数据库就绪：MySQL 容器刚起时可能还没接受连接，重试避免启动即崩溃重启。
	if err := pingWithRetry(db); err != nil {
		db.Close()
		return nil, err
	}
	if d.driver == "sqlite" {
		if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
			db.Close()
			return nil, fmt.Errorf("启用 SQLite 外键失败: %w", err)
		}
	}
	if err := execSchema(db, d.schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := cleanupLegacySchema(db, d); err != nil {
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
	var statements strings.Builder
	for _, line := range strings.Split(schema, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		statements.WriteString(line)
		statements.WriteByte('\n')
	}
	for _, stmt := range strings.Split(statements.String(), ";") {
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
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	sourceID, err := ensureShareSource(tx, Share{Provider: r.Provider, ShareURL: r.ShareURL, SharePwd: r.SharePwd}, false, now)
	if err != nil {
		return 0, err
	}
	r.ResourceKey = resourceKey(sourceID, r.FilePath)
	res, err := tx.Exec(
		`INSERT INTO resources (source_id, title, poster, overview, file_path, resource_key, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sourceID, r.Title, r.Poster, r.Overview, r.FilePath, r.ResourceKey, now, now,
	)
	if err != nil {
		if isDuplicateKey(err) {
			return 0, ErrResourceExists
		}
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const resourceCols = `r.id, r.source_id, r.title, COALESCE(r.poster,''), COALESCE(r.overview,''),
	COALESCE(s.title,''), COALESCE(s.category,''), s.provider, s.share_url, COALESCE(s.share_pwd,''), COALESCE(r.file_path,''), r.created_at, r.updated_at`

const resourceJoin = ` FROM resources r JOIN share_sources s ON s.id = r.source_id`

func scanResource(sc rowScanner) (Resource, error) {
	var r Resource
	err := sc.Scan(&r.ID, &r.SourceID, &r.Title, &r.Poster, &r.Overview, &r.SourceTitle, &r.SourceCategory, &r.Provider,
		&r.ShareURL, &r.SharePwd, &r.FilePath, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// ListResources 返回全部资源。
func (s *sqlStore) ListResources() ([]Resource, error) {
	rows, err := s.db.Query(`SELECT ` + resourceCols + resourceJoin + ` ORDER BY r.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Resource
	for rows.Next() {
		r, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetResource 按 ID 取资源。
func (s *sqlStore) GetResource(id int64) (Resource, error) {
	return scanResource(s.db.QueryRow(`SELECT `+resourceCols+resourceJoin+` WHERE r.id = ?`, id))
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
		`SELECT provider, token, extra, updated_at FROM provider_credentials WHERE provider = ?`, provider).
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
	rows, err := s.db.Query(`SELECT provider, token != '' FROM provider_credentials`)
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

func shareSourceKey(provider, shareURL string) string {
	shareURL = strings.TrimSpace(shareURL)
	if parsed, err := url.Parse(shareURL); err == nil {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.Fragment = ""
		shareURL = parsed.String()
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(provider)) + "\x00" + shareURL))
	return fmt.Sprintf("%x", sum)
}

func resourceKey(sourceID int64, filePath string) string {
	filePath = strings.TrimSpace(filePath)
	if filePath != "" {
		filePath = path.Clean("/" + filePath)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", sourceID, filePath)))
	return fmt.Sprintf("%x", sum)
}

func isDuplicateKey(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate entry")
}
