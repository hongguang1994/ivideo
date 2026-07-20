package config

import (
	"os"
	"strconv"
	"strings"
)

// Config 保存后端运行所需的全部配置，均来自环境变量。
type Config struct {
	Port string // 后端监听端口

	OpenListBaseURL  string // OpenList 服务地址，例如 http://openlist:5244
	OpenListUsername string // OpenList 登录用户名
	OpenListPassword string // OpenList 登录密码
	OpenListRoot     string // 视频根目录，例如 /videos

	JellyfinBaseURL string // Jellyfin 服务地址，例如 http://jellyfin:8096
	JellyfinAPIKey  string // Jellyfin 后台生成的 API Key

	// ---- strm 媒体库(给 Emby/Jellyfin 扫描)----
	MediaDir string // strm 落地目录(容器内路径)
	SiteURL  string // strm 里写的站点地址，如 http://192.168.50.140:8090
	// StrmMode 决定 strm 指向哪条通道：
	//   hls      —— 转码 HLS(默认)。阿里对免费账号的**原画下载限速**约 0.5MB/s，
	//               而转码流约 3.7MB/s，故默认用 HLS 保证流畅。
	//   original —— 原画直链。画质最好，适合开了会员/不限速的账号。
	StrmMode string

	// ---- 按需转存缓存 ----
	DBPath             string // SQLite 文件路径
	CacheBackend       string // 缓存盘适配器：fake / aliyun / ...
	CacheDir           string // 自己网盘里存缓存文件的目录
	CacheMaxBytes      int64  // 缓存总量上限，超过按 LRU 淘汰
	CacheTTLHours      int    // 超过多久没看就清理
	CacheCleanInterval int    // 清理任务间隔（分钟）

	// ---- 阿里云盘适配器（Share2Open 转存缓存）----
	AliyunRefreshToken     string // web 接口 refresh token（小雅的 mytoken.txt），用于转存/分享token
	AliyunOpenRefreshToken string // 开放接口 refresh token（小雅的 myopentoken.txt），用于取直链
	AliyunOpenClientID     string // 开放平台应用 client_id
	AliyunOpenClientSecret string // 开放平台应用 client_secret
	AliyunOpenTokenURL     string // 官方开放接口换 token 地址(填了自己的 client 时用)
	AliyunOpenRenewURL     string // 在线 token 服务(默认 OpenList 的 api.oplist.org)
	AliyunTempFolderID     string // 转存目标临时目录的 file_id（小雅的 temp_transfer_folder_id）
	AliyunDriveID          string // 自己盘 drive_id，留空则从 web token 自动获取

	// VideoExts 认定为视频的扩展名（小写，含点），用于过滤目录项。
	VideoExts []string
}

// JellyfinEnabled 表示是否配置了 Jellyfin（未配 API Key 则视为关闭）。
func (c Config) JellyfinEnabled() bool {
	return c.JellyfinBaseURL != "" && c.JellyfinAPIKey != ""
}

// Load 从环境变量读取配置，未设置的项使用合理默认值。
func Load() Config {
	return Config{
		Port:             env("SERVER_PORT", "3001"),
		OpenListBaseURL:  strings.TrimRight(env("OPENLIST_BASE_URL", "http://openlist:5244"), "/"),
		OpenListUsername: env("OPENLIST_USERNAME", "admin"),
		OpenListPassword: env("OPENLIST_PASSWORD", ""),
		OpenListRoot:     env("OPENLIST_ROOT", "/"),
		JellyfinBaseURL:  strings.TrimRight(env("JELLYFIN_BASE_URL", ""), "/"),
		JellyfinAPIKey:   env("JELLYFIN_API_KEY", ""),

		MediaDir: env("MEDIA_DIR", "/media"),
		SiteURL:  strings.TrimRight(env("SITE_URL", "http://localhost:8090"), "/"),
		StrmMode: env("STRM_MODE", "hls"),

		DBPath:             env("DB_PATH", "./ivideo.db"),
		CacheBackend:       env("CACHE_BACKEND", "fake"),
		CacheDir:           env("CACHE_DIR", "/ivideo-cache"),
		CacheMaxBytes:      envInt64("CACHE_MAX_BYTES", 200*1024*1024*1024), // 默认 200 GB
		CacheTTLHours:      envInt("CACHE_TTL_HOURS", 72),
		CacheCleanInterval: envInt("CACHE_CLEAN_INTERVAL_MINUTES", 10),

		AliyunRefreshToken:     env("ALIYUN_REFRESH_TOKEN", ""),
		AliyunOpenRefreshToken: env("ALIYUN_OPEN_REFRESH_TOKEN", ""),
		AliyunOpenClientID:     env("ALIYUN_OPEN_CLIENT_ID", ""),
		AliyunOpenClientSecret: env("ALIYUN_OPEN_CLIENT_SECRET", ""),
		AliyunOpenTokenURL:     env("ALIYUN_OPEN_TOKEN_URL", "https://openapi.alipan.com/oauth/access_token"),
		AliyunOpenRenewURL:     env("ALIYUN_OPEN_RENEW_URL", "https://api.oplist.org/alicloud/renewapi"),
		AliyunTempFolderID:     env("ALIYUN_TEMP_FOLDER_ID", "root"),
		AliyunDriveID:          env("ALIYUN_DRIVE_ID", ""),

		VideoExts: []string{".mp4", ".mkv", ".webm", ".mov", ".avi", ".flv", ".m4v", ".ts"},
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
