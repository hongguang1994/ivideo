package store

import (
	"embed"
	"fmt"
)

//go:embed schema.sql schema_mysql.sql
var schemaFS embed.FS

// dialect 收口各数据库的方言差异:建表 DDL + 4 条 upsert 语句。
// 其余 SQL(SELECT / UPDATE / DELETE、? 占位符、COALESCE)两库通用,不在此处。
type dialect struct {
	driver string // database/sql 驱动名
	schema string // 建表 DDL(Open 时按 ; 拆分逐条执行)

	upsertReady      string // SetReady
	upsertStatus     string // SetTransferring / SetFailed
	upsertCredential string // SetCredential
	upsertCredToken  string // SetCredentialToken
}

func mustSchema(name string) string {
	b, err := schemaFS.ReadFile(name)
	if err != nil {
		panic(err) // 编译期 embed,读不到属编程错误
	}
	return string(b)
}

// sqliteDialect —— SQLite:ON CONFLICT ... DO UPDATE SET x=excluded.x
var sqliteDialect = dialect{
	driver: "sqlite",
	schema: mustSchema("schema.sql"),
	upsertReady: `INSERT INTO cache_items (resource_id, backend, status, cache_path, direct_url, size, last_access, error, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)
		 ON CONFLICT(resource_id) DO UPDATE SET
		    backend=excluded.backend, status=excluded.status, cache_path=excluded.cache_path,
		    direct_url=excluded.direct_url, size=excluded.size, last_access=excluded.last_access,
		    error='', updated_at=excluded.updated_at`,
	upsertStatus: `INSERT INTO cache_items (resource_id, backend, status, size, last_access, error, updated_at)
		 VALUES (?, ?, ?, 0, 0, ?, ?)
		 ON CONFLICT(resource_id) DO UPDATE SET
		    backend=excluded.backend, status=excluded.status, error=excluded.error, updated_at=excluded.updated_at`,
	upsertCredential: `INSERT INTO credentials (provider, token, extra, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(provider) DO UPDATE SET token=excluded.token, extra=excluded.extra, updated_at=excluded.updated_at`,
	upsertCredToken: `INSERT INTO credentials (provider, token, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(provider) DO UPDATE SET token=excluded.token, updated_at=excluded.updated_at`,
}

// mysqlDialect —— MySQL:ON DUPLICATE KEY UPDATE x=VALUES(x)
var mysqlDialect = dialect{
	driver: "mysql",
	schema: mustSchema("schema_mysql.sql"),
	upsertReady: `INSERT INTO cache_items (resource_id, backend, status, cache_path, direct_url, size, last_access, error, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)
		 ON DUPLICATE KEY UPDATE
		    backend=VALUES(backend), status=VALUES(status), cache_path=VALUES(cache_path),
		    direct_url=VALUES(direct_url), size=VALUES(size), last_access=VALUES(last_access),
		    error='', updated_at=VALUES(updated_at)`,
	upsertStatus: `INSERT INTO cache_items (resource_id, backend, status, size, last_access, error, updated_at)
		 VALUES (?, ?, ?, 0, 0, ?, ?)
		 ON DUPLICATE KEY UPDATE
		    backend=VALUES(backend), status=VALUES(status), error=VALUES(error), updated_at=VALUES(updated_at)`,
	upsertCredential: `INSERT INTO credentials (provider, token, extra, updated_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE token=VALUES(token), extra=VALUES(extra), updated_at=VALUES(updated_at)`,
	upsertCredToken: `INSERT INTO credentials (provider, token, updated_at) VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE token=VALUES(token), updated_at=VALUES(updated_at)`,
}

// dialectFor 按驱动名返回方言。
func dialectFor(driver string) (dialect, error) {
	switch driver {
	case "", "sqlite":
		return sqliteDialect, nil
	case "mysql":
		return mysqlDialect, nil
	default:
		return dialect{}, fmt.Errorf("不支持的数据库驱动: %s(支持 sqlite / mysql)", driver)
	}
}
