package store

import (
	"database/sql"
	"fmt"
)

// upgradeCacheSchema adds additive cache-state columns for installations whose
// cache_items table already existed before this version. CREATE TABLE IF NOT
// EXISTS cannot evolve an existing table, so upgrades are explicit and safe.
func upgradeCacheSchema(db *sql.DB, d dialect) error {
	for _, column := range []struct {
		name       string
		definition string
	}{
		{"fail_count", "INTEGER NOT NULL DEFAULT 0"},
		{"next_retry_at", "INTEGER NOT NULL DEFAULT 0"},
	} {
		exists, err := cacheColumnExists(db, d, column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		definition := column.definition
		if d.driver == "mysql" {
			definition = map[string]string{
				"fail_count":    "INT NOT NULL DEFAULT 0",
				"next_retry_at": "BIGINT NOT NULL DEFAULT 0",
			}[column.name]
		}
		if _, err := db.Exec("ALTER TABLE cache_items ADD COLUMN " + column.name + " " + definition); err != nil {
			return fmt.Errorf("升级 cache_items.%s: %w", column.name, err)
		}
	}
	return nil
}

func cacheColumnExists(db *sql.DB, d dialect, column string) (bool, error) {
	if d.driver == "mysql" {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema=DATABASE() AND table_name='cache_items' AND column_name=?`, column).Scan(&count)
		return count > 0, err
	}
	rows, err := db.Query(`PRAGMA table_info(cache_items)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
