package config

import "testing"

func TestValidateRequiresMySQLDSN(t *testing.T) {
	cfg := Config{DBDriver: "mysql", StrmMode: "hls", SiteURL: "http://web"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("MySQL 缺少 DSN 时应校验失败")
	}
}

func TestValidateAllowsExplicitSQLite(t *testing.T) {
	cfg := Config{
		DBDriver: "sqlite",
		DBPath:   "./ivideo.db",
		StrmMode: "hls",
		SiteURL:  "http://localhost:8090",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("显式 SQLite 本地开发配置不应失败: %v", err)
	}
}

func TestValidateRejectsUnknownStrmMode(t *testing.T) {
	cfg := Config{DBDriver: "mysql", DBDSN: "user:pwd@tcp(mysql:3306)/ivideo", StrmMode: "dash", SiteURL: "http://web"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("未知 strm.mode 应校验失败")
	}
}
