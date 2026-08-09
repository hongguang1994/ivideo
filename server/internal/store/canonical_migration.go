package store

import (
	"database/sql"
	"fmt"
	"time"
)

// backfillCanonicalShareCatalog is additive and idempotent. Existing features
// still use share_sources/resources while the new normalized catalog gains a
// complete history, so deployment never requires a destructive table swap.
func backfillCanonicalShareCatalog(db *sql.DB, d dialect) error {
	now := time.Now().Unix()
	legacySourceInsert := `INSERT OR IGNORE INTO discovery_sources (source_type, source_key, display_name, enabled, config_json, created_at, updated_at)
		VALUES ('legacy', 'share_sources', '历史分享库', 1, '{}', ?, ?)`
	shareInsert := `INSERT OR IGNORE INTO shares (provider, share_url, share_pwd, share_id, canonical_key, legacy_source_id, status, first_seen_at, last_seen_at)
		SELECT provider, share_url, COALESCE(share_pwd,''), COALESCE(share_id,''), source_key, id, status, created_at, updated_at FROM share_sources`
	legacyObservationInsert := `INSERT OR IGNORE INTO share_observations (share_id, discovery_source_id, source_ref, source_ref_key, title, category, file_name, metadata_json, active, first_seen_at, last_seen_at)
		SELECT sh.id, ds.id, CAST(ss.id AS TEXT), CAST(ss.id AS TEXT), COALESCE(ss.title,''), COALESCE(ss.category,''), '', '{}', 1, ss.created_at, ss.updated_at
		FROM share_sources ss JOIN shares sh ON sh.legacy_source_id=ss.id
		JOIN discovery_sources ds ON ds.source_type='legacy' AND ds.source_key='share_sources'`
	githubSourceInsert := `INSERT OR IGNORE INTO discovery_sources (source_type, source_key, display_name, enabled, config_json, created_at, updated_at)
		SELECT 'github', repository, repository, enabled, '{}', created_at, updated_at FROM github_repositories`
	githubObservationInsert := `INSERT OR IGNORE INTO share_observations (share_id, discovery_source_id, source_ref, source_ref_key, title, category, file_name, metadata_json, active, first_seen_at, last_seen_at)
		SELECT sh.id, ds.id, f.path, f.path, o.title, o.resource_type, o.file_name, '{}', o.active, o.observed_at, o.observed_at
		FROM github_share_observations o
		JOIN github_repository_files f ON f.id=o.repository_file_id
		JOIN github_repositories gr ON gr.id=f.repository_id
		JOIN share_sources ss ON ss.id=o.source_id
		JOIN shares sh ON sh.legacy_source_id=ss.id
		JOIN discovery_sources ds ON ds.source_type='github' AND ds.source_key=gr.repository`
	healthInsert := `INSERT INTO share_health_checks (share_id, status, entry_count, total_size, message, checked_at)
		SELECT sh.id, ss.status, ss.file_count, ss.total_size, '', ss.last_checked_at
		FROM share_sources ss JOIN shares sh ON sh.legacy_source_id=ss.id
		WHERE ss.last_checked_at>0 AND NOT EXISTS (
			SELECT 1 FROM share_health_checks hc WHERE hc.share_id=sh.id AND hc.checked_at=ss.last_checked_at)`
	if d.driver == "mysql" {
		legacySourceInsert = `INSERT IGNORE INTO discovery_sources (source_type, source_key, display_name, enabled, config_json, created_at, updated_at)
			VALUES ('legacy', 'share_sources', '历史分享库', 1, '{}', ?, ?)`
		shareInsert = `INSERT IGNORE INTO shares (provider, share_url, share_pwd, share_id, canonical_key, legacy_source_id, status, first_seen_at, last_seen_at)
			SELECT provider, share_url, COALESCE(share_pwd,''), COALESCE(share_id,''), source_key, id, status, created_at, updated_at FROM share_sources`
		legacyObservationInsert = `INSERT IGNORE INTO share_observations (share_id, discovery_source_id, source_ref, source_ref_key, title, category, file_name, metadata_json, active, first_seen_at, last_seen_at)
			SELECT sh.id, ds.id, CAST(ss.id AS CHAR), SHA2(CAST(ss.id AS CHAR), 256), COALESCE(ss.title,''), COALESCE(ss.category,''), '', '{}', 1, ss.created_at, ss.updated_at
			FROM share_sources ss JOIN shares sh ON sh.legacy_source_id=ss.id
			JOIN discovery_sources ds ON ds.source_type='legacy' AND ds.source_key='share_sources'`
		githubSourceInsert = `INSERT IGNORE INTO discovery_sources (source_type, source_key, display_name, enabled, config_json, created_at, updated_at)
			SELECT 'github', repository, repository, enabled, '{}', created_at, updated_at FROM github_repositories`
		githubObservationInsert = `INSERT IGNORE INTO share_observations (share_id, discovery_source_id, source_ref, source_ref_key, title, category, file_name, metadata_json, active, first_seen_at, last_seen_at)
			SELECT sh.id, ds.id, f.path, SHA2(f.path, 256), o.title, o.resource_type, o.file_name, '{}', o.active, o.observed_at, o.observed_at
			FROM github_share_observations o
			JOIN github_repository_files f ON f.id=o.repository_file_id
			JOIN github_repositories gr ON gr.id=f.repository_id
			JOIN share_sources ss ON ss.id=o.source_id
			JOIN shares sh ON sh.legacy_source_id=ss.id
			JOIN discovery_sources ds ON ds.source_type='github' AND ds.source_key=gr.repository`
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{legacySourceInsert, []any{now, now}},
		{shareInsert, nil},
		{legacyObservationInsert, nil},
		{githubSourceInsert, nil},
		{githubObservationInsert, nil},
		{healthInsert, nil},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			return fmt.Errorf("回填规范化资源目录: %w", err)
		}
	}
	return nil
}
