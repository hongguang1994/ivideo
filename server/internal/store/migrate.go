package store

import (
	"database/sql"
	"fmt"
)

// Migrate 把源库(fromDriver/fromDSN)的全部数据复制到目标库(toDriver/toDSN)。
// 目标库自动建表;应迁入【空库】(同主键已存在会报错,避免误合并)。
// 保留资源 ID —— strm/缓存都按资源 ID 关联,ID 必须一致。
func Migrate(fromDriver, fromDSN, toDriver, toDSN string) (map[string]int, error) {
	fromD, err := dialectFor(fromDriver)
	if err != nil {
		return nil, err
	}
	toD, err := dialectFor(toDriver)
	if err != nil {
		return nil, err
	}

	src, err := sql.Open(fromD.driver, fromDSN)
	if err != nil {
		return nil, fmt.Errorf("打开源库失败: %w", err)
	}
	defer src.Close()
	if err := src.Ping(); err != nil {
		return nil, fmt.Errorf("连接源库失败: %w", err)
	}

	dst, err := sql.Open(toD.driver, toDSN)
	if err != nil {
		return nil, fmt.Errorf("打开目标库失败: %w", err)
	}
	defer dst.Close()
	if err := dst.Ping(); err != nil {
		return nil, fmt.Errorf("连接目标库失败: %w", err)
	}

	if err := execSchema(dst, toD.schema); err != nil {
		return nil, fmt.Errorf("目标库建表失败: %w", err)
	}

	counts := map[string]int{}
	for _, t := range []struct {
		name string
		fn   func(src, dst *sql.DB) (int, error)
	}{
		{"resources", migrateResources},
		{"cache_items", migrateCacheItems},
		{"credentials", migrateCredentials},
	} {
		n, err := t.fn(src, dst)
		counts[t.name] = n
		if err != nil {
			return counts, fmt.Errorf("迁移 %s 失败(已迁 %d 行): %w", t.name, n, err)
		}
	}
	return counts, nil
}

func migrateResources(src, dst *sql.DB) (int, error) {
	rows, err := src.Query(`SELECT id, title, COALESCE(poster,''), COALESCE(overview,''), provider,
		share_url, COALESCE(share_pwd,''), COALESCE(file_path,''), created_at FROM resources`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, createdAt int64
		var title, poster, overview, provider, shareURL, sharePwd, filePath string
		if err := rows.Scan(&id, &title, &poster, &overview, &provider, &shareURL, &sharePwd, &filePath, &createdAt); err != nil {
			return n, err
		}
		if _, err := dst.Exec(`INSERT INTO resources (id, title, poster, overview, provider, share_url, share_pwd, file_path, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, title, poster, overview, provider, shareURL, sharePwd, filePath, createdAt); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func migrateCacheItems(src, dst *sql.DB) (int, error) {
	rows, err := src.Query(`SELECT resource_id, backend, status, COALESCE(cache_path,''), COALESCE(direct_url,''),
		size, last_access, COALESCE(error,''), updated_at FROM cache_items`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var resourceID, size, lastAccess, updatedAt int64
		var backend, status, cachePath, directURL, errMsg string
		if err := rows.Scan(&resourceID, &backend, &status, &cachePath, &directURL, &size, &lastAccess, &errMsg, &updatedAt); err != nil {
			return n, err
		}
		if _, err := dst.Exec(`INSERT INTO cache_items (resource_id, backend, status, cache_path, direct_url, size, last_access, error, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			resourceID, backend, status, cachePath, directURL, size, lastAccess, errMsg, updatedAt); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func migrateCredentials(src, dst *sql.DB) (int, error) {
	rows, err := src.Query(`SELECT provider, token, extra, updated_at FROM credentials`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var provider, token, extra string
		var updatedAt int64
		if err := rows.Scan(&provider, &token, &extra, &updatedAt); err != nil {
			return n, err
		}
		if _, err := dst.Exec(`INSERT INTO credentials (provider, token, extra, updated_at) VALUES (?, ?, ?, ?)`,
			provider, token, extra, updatedAt); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}
