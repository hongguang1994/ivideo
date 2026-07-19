package config

import (
	"os"
	"strings"
)

// Config 保存后端运行所需的全部配置，均来自环境变量。
type Config struct {
	Port string // 后端监听端口

	OpenListBaseURL  string // OpenList 服务地址，例如 http://openlist:5244
	OpenListUsername string // OpenList 登录用户名
	OpenListPassword string // OpenList 登录密码
	OpenListRoot     string // 视频根目录，例如 /videos

	// VideoExts 认定为视频的扩展名（小写，含点），用于过滤目录项。
	VideoExts []string
}

// Load 从环境变量读取配置，未设置的项使用合理默认值。
func Load() Config {
	return Config{
		Port:             env("SERVER_PORT", "3001"),
		OpenListBaseURL:  strings.TrimRight(env("OPENLIST_BASE_URL", "http://openlist:5244"), "/"),
		OpenListUsername: env("OPENLIST_USERNAME", "admin"),
		OpenListPassword: env("OPENLIST_PASSWORD", ""),
		OpenListRoot:     env("OPENLIST_ROOT", "/"),
		VideoExts:        []string{".mp4", ".mkv", ".webm", ".mov", ".avi", ".flv", ".m4v", ".ts"},
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
