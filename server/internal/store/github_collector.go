package store

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GitHubRepository is an approved repository in the local collection plan.
type GitHubRepository struct {
	ID              int64  `json:"id"`
	Repository      string `json:"repository"`
	Branch          string `json:"branch"`
	Parser          string `json:"parser"`
	Enabled         bool   `json:"enabled"`
	LastCommitSHA   string `json:"lastCommitSha"`
	LastCollectedAt int64  `json:"lastCollectedAt"`
	LastError       string `json:"lastError"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

func extractShareIDFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) == 0 {
		return ""
	}
	return segments[len(segments)-1]
}

type GitHubRepositoryFile struct {
	ID           int64  `json:"id"`
	RepositoryID int64  `json:"repositoryId"`
	Path         string `json:"path"`
	BlobSHA      string `json:"blobSha"`
	Size         int64  `json:"size"`
	Active       bool   `json:"active"`
}

// GitHubObservedShare carries structured data parsed from a repository file.
type GitHubObservedShare struct {
	Provider     string `json:"provider"`
	ShareURL     string `json:"shareUrl"`
	SharePwd     string `json:"sharePwd"`
	Title        string `json:"title"`
	ResourceType string `json:"resourceType"`
	FileName     string `json:"fileName"`
	UpdatedAt    string `json:"updatedAt"`
	Repository   string `json:"repository"`
	Path         string `json:"path"`
	SourceURL    string `json:"sourceUrl"`
}

type GitHubCollectedFile struct {
	Path       string
	BlobSHA    string
	Size       int64
	Shares     []GitHubObservedShare
	ParseError string
}

// GitHubRepositorySnapshot applies only changed files. RemovedPaths are
// retained as history but no longer contribute active resource observations.
type GitHubRepositorySnapshot struct {
	Repository      string
	Branch          string
	Parser          string
	CommitSHA       string
	Files           []GitHubCollectedFile
	RemovedPaths    []string
	CollectionError string
}

type GitHubCollectionResult struct {
	FilesUpdated int `json:"filesUpdated"`
	SharesAdded  int `json:"sharesAdded"`
	SharesKnown  int `json:"sharesKnown"`
}

func githubRepositoryPathKey(filePath string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(filePath)))
}

func discoverySourceRefKey(sourceRef string) string { return githubRepositoryPathKey(sourceRef) }

func (s *sqlStore) ensureDiscoverySource(tx *sql.Tx, sourceType, sourceKey, displayName string, now int64) (int64, error) {
	insert := `INSERT INTO discovery_sources (source_type, source_key, display_name, enabled, config_json, created_at, updated_at)
		VALUES (?, ?, ?, 1, '{}', ?, ?) ON CONFLICT(source_type, source_key) DO NOTHING`
	if s.d.driver == "mysql" {
		insert = `INSERT IGNORE INTO discovery_sources (source_type, source_key, display_name, enabled, config_json, created_at, updated_at)
			VALUES (?, ?, ?, 1, '{}', ?, ?)`
	}
	if _, err := tx.Exec(insert, sourceType, sourceKey, displayName, now, now); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM discovery_sources WHERE source_type=? AND source_key=?`, sourceType, sourceKey).Scan(&id)
	return id, err
}

func (s *sqlStore) ensureCanonicalShare(tx *sql.Tx, observed GitHubObservedShare, now int64) (int64, error) {
	key := shareSourceKey(observed.Provider, observed.ShareURL)
	if key == "" || key == "\x00" {
		return 0, nil
	}
	insert := `INSERT INTO shares (provider, share_url, share_pwd, share_id, canonical_key, status, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, 'unknown', ?, ?)
		ON CONFLICT(canonical_key) DO UPDATE SET share_pwd=CASE WHEN excluded.share_pwd<>'' THEN excluded.share_pwd ELSE shares.share_pwd END,
		last_seen_at=excluded.last_seen_at`
	if s.d.driver == "mysql" {
		insert = `INSERT INTO shares (provider, share_url, share_pwd, share_id, canonical_key, status, first_seen_at, last_seen_at)
			VALUES (?, ?, ?, ?, ?, 'unknown', ?, ?)
			ON DUPLICATE KEY UPDATE share_pwd=IF(VALUES(share_pwd)<>'', VALUES(share_pwd), share_pwd), last_seen_at=VALUES(last_seen_at)`
	}
	if _, err := tx.Exec(insert, observed.Provider, observed.ShareURL, observed.SharePwd, extractShareIDFromURL(observed.ShareURL), key, now, now); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM shares WHERE canonical_key=?`, key).Scan(&id)
	return id, err
}

func (s *sqlStore) upsertCanonicalObservation(tx *sql.Tx, shareID, discoverySourceID int64, sourceRef string, observed GitHubObservedShare, now int64) error {
	if shareID == 0 {
		return nil
	}
	upsert := `INSERT INTO share_observations (share_id, discovery_source_id, source_ref, source_ref_key, title, category, file_name, metadata_json, active, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}', 1, ?, ?)
		ON CONFLICT(share_id, discovery_source_id, source_ref_key) DO UPDATE SET title=excluded.title, category=excluded.category,
		file_name=excluded.file_name, active=1, last_seen_at=excluded.last_seen_at`
	if s.d.driver == "mysql" {
		upsert = `INSERT INTO share_observations (share_id, discovery_source_id, source_ref, source_ref_key, title, category, file_name, metadata_json, active, first_seen_at, last_seen_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, '{}', 1, ?, ?)
			ON DUPLICATE KEY UPDATE title=VALUES(title), category=VALUES(category), file_name=VALUES(file_name), active=1, last_seen_at=VALUES(last_seen_at)`
	}
	_, err := tx.Exec(upsert, shareID, discoverySourceID, sourceRef, discoverySourceRefKey(sourceRef), observed.Title, observed.ResourceType, observed.FileName, now, now)
	return err
}

func (s *sqlStore) syncCanonicalLegacySource(tx *sql.Tx, sourceID, now int64) error {
	var sh Share
	err := tx.QueryRow(`SELECT `+shareCols+` FROM share_sources WHERE id=?`, sourceID).Scan(&sh.ID, &sh.Provider, &sh.ShareURL, &sh.SharePwd, &sh.ShareID,
		&sh.Title, &sh.Remark, &sh.Category, &sh.Status, &sh.LastCheckedAt, &sh.FileCount, &sh.TotalSize, &sh.CreatedAt, &sh.UpdatedAt)
	if err != nil {
		return err
	}
	discoverySourceID, err := s.ensureDiscoverySource(tx, "legacy", "share_sources", "历史分享库", now)
	if err != nil {
		return err
	}
	observed := GitHubObservedShare{Provider: sh.Provider, ShareURL: sh.ShareURL, SharePwd: sh.SharePwd, Title: sh.Title, ResourceType: sh.Category}
	canonicalID, err := s.ensureCanonicalShare(tx, observed, now)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE shares SET legacy_source_id=?, status=?, last_seen_at=? WHERE id=?`, sourceID, sh.Status, now, canonicalID); err != nil {
		return err
	}
	if err := s.upsertCanonicalObservation(tx, canonicalID, discoverySourceID, strconv.FormatInt(sourceID, 10), observed, now); err != nil {
		return err
	}
	if sh.LastCheckedAt > 0 {
		if _, err := tx.Exec(`INSERT INTO share_health_checks (share_id, status, entry_count, total_size, message, checked_at)
			SELECT ?, ?, ?, ?, '', ? WHERE NOT EXISTS (SELECT 1 FROM share_health_checks WHERE share_id=? AND checked_at=?)`,
			canonicalID, sh.Status, sh.FileCount, sh.TotalSize, sh.LastCheckedAt, canonicalID, sh.LastCheckedAt); err != nil {
			return err
		}
	}
	return nil
}

func scanGitHubRepository(sc rowScanner) (GitHubRepository, error) {
	var r GitHubRepository
	err := sc.Scan(&r.ID, &r.Repository, &r.Branch, &r.Parser, &r.Enabled, &r.LastCommitSHA,
		&r.LastCollectedAt, &r.LastError, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (s *sqlStore) EnsureGitHubRepository(repository, branch, parser string) (GitHubRepository, error) {
	now := time.Now().Unix()
	_, err := s.db.Exec(`INSERT INTO github_repositories (repository, branch, parser, enabled, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)`, repository, branch, parser, now, now)
	if err != nil && !isDuplicateKey(err) {
		return GitHubRepository{}, err
	}
	return scanGitHubRepository(s.db.QueryRow(`SELECT id, repository, branch, parser, enabled, last_commit_sha,
		last_collected_at, last_error, created_at, updated_at FROM github_repositories WHERE repository=?`, repository))
}

func (s *sqlStore) ListGitHubRepositories() ([]GitHubRepository, error) {
	rows, err := s.db.Query(`SELECT id, repository, branch, parser, enabled, last_commit_sha,
		last_collected_at, last_error, created_at, updated_at FROM github_repositories ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]GitHubRepository, 0)
	for rows.Next() {
		item, err := scanGitHubRepository(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) GetGitHubRepositoryFiles(repositoryID int64) ([]GitHubRepositoryFile, error) {
	rows, err := s.db.Query(`SELECT id, repository_id, path, blob_sha, size, active
		FROM github_repository_files WHERE repository_id=?`, repositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]GitHubRepositoryFile, 0)
	for rows.Next() {
		var item GitHubRepositoryFile
		if err := rows.Scan(&item.ID, &item.RepositoryID, &item.Path, &item.BlobSHA, &item.Size, &item.Active); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) ApplyGitHubRepositorySnapshot(snapshot GitHubRepositorySnapshot) (GitHubCollectionResult, error) {
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return GitHubCollectionResult{}, err
	}
	defer tx.Rollback()
	var repositoryID int64
	err = tx.QueryRow(`SELECT id FROM github_repositories WHERE repository=?`, snapshot.Repository).Scan(&repositoryID)
	if err == sql.ErrNoRows {
		res, insertErr := tx.Exec(`INSERT INTO github_repositories (repository, branch, parser, enabled, created_at, updated_at)
			VALUES (?, ?, ?, 1, ?, ?)`, snapshot.Repository, snapshot.Branch, snapshot.Parser, now, now)
		if insertErr != nil {
			return GitHubCollectionResult{}, insertErr
		}
		repositoryID, err = res.LastInsertId()
	}
	if err != nil {
		return GitHubCollectionResult{}, err
	}
	discoverySourceID, err := s.ensureDiscoverySource(tx, "github", snapshot.Repository, snapshot.Repository, now)
	if err != nil {
		return GitHubCollectionResult{}, err
	}
	result := GitHubCollectionResult{}
	for _, path := range snapshot.RemovedPaths {
		if _, err := tx.Exec(`UPDATE github_repository_files SET active=0 WHERE repository_id=? AND path=?`, repositoryID, path); err != nil {
			return result, err
		}
		if _, err := tx.Exec(`UPDATE share_observations SET active=0 WHERE discovery_source_id=? AND source_ref=?`, discoverySourceID, path); err != nil {
			return result, err
		}
	}
	for _, collected := range snapshot.Files {
		var fileID int64
		err := tx.QueryRow(`SELECT id FROM github_repository_files WHERE repository_id=? AND path=?`, repositoryID, collected.Path).Scan(&fileID)
		if err == sql.ErrNoRows {
			res, insertErr := tx.Exec(`INSERT INTO github_repository_files (repository_id, path, path_key, blob_sha, size, active, last_collected_at, last_error)
				VALUES (?, ?, ?, ?, ?, 1, ?, ?)`, repositoryID, collected.Path, githubRepositoryPathKey(collected.Path), collected.BlobSHA, collected.Size, now, collected.ParseError)
			if insertErr != nil {
				return result, insertErr
			}
			fileID, err = res.LastInsertId()
		} else if err == nil {
			_, err = tx.Exec(`UPDATE github_repository_files SET blob_sha=?, size=?, active=1, last_collected_at=?, last_error=? WHERE id=?`,
				collected.BlobSHA, collected.Size, now, collected.ParseError, fileID)
		}
		if err != nil {
			return result, err
		}
		result.FilesUpdated++
		if collected.ParseError == "" {
			if _, err := tx.Exec(`UPDATE github_share_observations SET active=0 WHERE repository_file_id=?`, fileID); err != nil {
				return result, err
			}
			if _, err := tx.Exec(`UPDATE share_observations SET active=0 WHERE discovery_source_id=? AND source_ref=?`, discoverySourceID, collected.Path); err != nil {
				return result, err
			}
		}
		for _, observed := range collected.Shares {
			key := shareSourceKey(observed.Provider, observed.ShareURL)
			if key == "" || key == "\x00" {
				continue
			}
			var prior int64
			lookupErr := tx.QueryRow(`SELECT id FROM share_sources WHERE source_key=?`, key).Scan(&prior)
			if lookupErr == sql.ErrNoRows {
				result.SharesAdded++
			} else if lookupErr == nil {
				result.SharesKnown++
			} else {
				return result, lookupErr
			}
			sourceID, ensureErr := ensureShareSource(tx, Share{Provider: observed.Provider, ShareURL: observed.ShareURL,
				SharePwd: observed.SharePwd, ShareID: extractShareIDFromURL(observed.ShareURL), Title: observed.Title, Category: observed.ResourceType}, false, now)
			if ensureErr != nil {
				return result, ensureErr
			}
			canonicalShareID, canonicalErr := s.ensureCanonicalShare(tx, observed, now)
			if canonicalErr != nil {
				return result, canonicalErr
			}
			if err := s.upsertCanonicalObservation(tx, canonicalShareID, discoverySourceID, collected.Path, observed, now); err != nil {
				return result, err
			}
			upsert := `INSERT INTO github_share_observations (repository_file_id, source_id, title, resource_type, file_name, updated_at_text, active, observed_at)
				VALUES (?, ?, ?, ?, ?, ?, 1, ?)
				ON CONFLICT(repository_file_id, source_id) DO UPDATE SET title=excluded.title, resource_type=excluded.resource_type,
				file_name=excluded.file_name, updated_at_text=excluded.updated_at_text, active=1, observed_at=excluded.observed_at`
			if s.d.driver == "mysql" {
				upsert = `INSERT INTO github_share_observations (repository_file_id, source_id, title, resource_type, file_name, updated_at_text, active, observed_at)
					VALUES (?, ?, ?, ?, ?, ?, 1, ?)
					ON DUPLICATE KEY UPDATE title=VALUES(title), resource_type=VALUES(resource_type), file_name=VALUES(file_name),
					updated_at_text=VALUES(updated_at_text), active=1, observed_at=VALUES(observed_at)`
			}
			_, err = tx.Exec(upsert, fileID, sourceID, observed.Title, observed.ResourceType, observed.FileName, observed.UpdatedAt, now)
			if err != nil {
				return result, err
			}
		}
	}
	_, err = tx.Exec(`UPDATE github_repositories SET branch=?, parser=?, last_commit_sha=?, last_collected_at=?, last_error=?, updated_at=? WHERE id=?`,
		snapshot.Branch, snapshot.Parser, snapshot.CommitSHA, now, snapshot.CollectionError, now, repositoryID)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func (s *sqlStore) ListGitHubObservedShares(page, pageSize int) ([]GitHubObservedShare, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM github_share_observations o
		JOIN github_repository_files f ON f.id=o.repository_file_id
		JOIN github_repositories r ON r.id=f.repository_id
		WHERE o.active=1 AND f.active=1 AND r.enabled=1`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT s.provider, s.share_url, COALESCE(s.share_pwd,''), o.title, o.resource_type,
		COALESCE(o.file_name,''), COALESCE(o.updated_at_text,''), r.repository, f.path
		FROM github_share_observations o
		JOIN github_repository_files f ON f.id=o.repository_file_id
		JOIN github_repositories r ON r.id=f.repository_id
		JOIN share_sources s ON s.id=o.source_id
		WHERE o.active=1 AND f.active=1 AND r.enabled=1
		ORDER BY o.observed_at DESC, o.title ASC LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]GitHubObservedShare, 0)
	for rows.Next() {
		var item GitHubObservedShare
		if err := rows.Scan(&item.Provider, &item.ShareURL, &item.SharePwd, &item.Title, &item.ResourceType, &item.FileName, &item.UpdatedAt, &item.Repository, &item.Path); err != nil {
			return nil, 0, err
		}
		item.SourceURL = "https://github.com/" + item.Repository + "/blob/HEAD/" + item.Path
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *sqlStore) ListGitHubCatalogShares() ([]GitHubObservedShare, error) {
	rows, err := s.db.Query(`SELECT s.provider, s.share_url, COALESCE(s.share_pwd,''), o.title, o.resource_type,
		COALESCE(o.file_name,''), COALESCE(o.updated_at_text,''), r.repository, f.path
		FROM github_share_observations o
		JOIN github_repository_files f ON f.id=o.repository_file_id
		JOIN github_repositories r ON r.id=f.repository_id
		JOIN share_sources s ON s.id=o.source_id
		WHERE o.active=1 AND f.active=1 AND r.enabled=1
		ORDER BY o.observed_at DESC, o.title ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]GitHubObservedShare, 0)
	for rows.Next() {
		var item GitHubObservedShare
		if err := rows.Scan(&item.Provider, &item.ShareURL, &item.SharePwd, &item.Title, &item.ResourceType, &item.FileName, &item.UpdatedAt, &item.Repository, &item.Path); err != nil {
			return nil, err
		}
		item.SourceURL = "https://github.com/" + item.Repository + "/blob/HEAD/" + item.Path
		items = append(items, item)
	}
	return items, rows.Err()
}
