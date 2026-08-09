package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config 保存后端运行所需的全部配置（来自默认值 / 配置文件 / 环境变量）。
type Config struct {
	Port   string // 后端监听端口
	APIKey string // 内网 API 访问密钥；留空只允许本地开发

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
	// OriginalMaxMbps 是原画通道能稳定喂动的最大码率(Mbps)，
	// 取决于开放接口令牌属于哪个阿里应用 —— 阿里**按 client_id 限速**：
	//   oplist·TV 版        实测 55~66 Mbps
	//   AList / oplist·OAuth2 实测约 4 Mbps
	// 码率超过它的片源自动改走转码流(转码流不限速)，避免硬走原画卡成幻灯片。
	// 0 = 关闭自动选流，一律按 StrmMode 走。
	OriginalMaxMbps float64
	// StrmAutoInterval 是 strm 媒体库定时兜底重建的间隔（分钟）。
	// 导入完成后会即时重建，这里只是覆盖「不经导入接口的资源变动」。<=0 表示只在启动时生成一次。
	StrmAutoInterval int

	// ---- 数据库 ----
	DBDriver string // mysql(正式部署) / sqlite(显式本地开发)
	DBDSN    string // mysql 用:user:pass@tcp(host:3306)/ivideo?charset=utf8mb4

	// ---- 按需转存缓存 ----
	DBPath             string // SQLite 文件路径(driver=sqlite 时用)
	CacheBackend       string // 缓存盘适配器：fake / aliyun / ...
	CacheDir           string // 自己网盘里存缓存文件的目录
	CacheMaxBytes      int64  // 缓存总量上限，超过按 LRU 淘汰
	CacheTTLHours      int    // 超过多久没看就清理
	CacheCleanInterval int    // 清理任务间隔（分钟）
	CacheStopGrace     int    // 停止播放后多久删（分钟，会话感知）
	TokenRefreshMin    int    // 令牌保活定时刷新间隔（分钟，0=关闭）
	ShareCheckInterval int    // 分享可用性检查间隔（分钟，0=关闭）

	// ---- 独立资源发现引擎 ----
	DiscoveryConcurrency      int      // 同时执行的来源插件数
	DiscoveryTimeoutSeconds   int      // 单个来源超时
	DiscoveryQuickWaitSeconds int      // 首屏最多等待时间，慢来源在后台继续
	DiscoveryCacheMinutes     int      // 查询结果缓存时间
	DiscoveryMaxResults       int      // 单次返回结果上限
	DiscoveryTelegramChannels []string // Telegram 公开频道（不需要登录凭据）

	// ---- 分享导入 ----
	ImportMaxDepth int // 导入分享时最大递归深度
	ImportMaxFiles int // 单次导入最多多少个视频

	// ---- 阿里云盘适配器（Share2Open 转存缓存）----
	AliyunRefreshToken     string // web 接口 refresh token（小雅的 mytoken.txt），用于转存/分享token
	AliyunOpenRefreshToken string // 开放接口 refresh token（小雅的 myopentoken.txt），用于取直链
	AliyunOpenClientID     string // 开放平台应用 client_id
	AliyunOpenClientSecret string // 开放平台应用 client_secret
	AliyunOpenTokenURL     string // 官方开放接口换 token 地址(填了自己的 client 时用)
	AliyunOpenRenewURL     string // 在线 token 服务(默认 OpenList 的 api.oplist.org)
	// AliyunOpenRenewStyle 是在线 token 服务的调用形态，取决于用哪一家：
	//   alist  —— api.alistgo.com，POST {"grant_type","refresh_token"}（默认）
	//   oplist —— api.oplist.org，GET ?refresh_ui=&driver_txt=
	// 两家是各自注册的开放平台应用，阿里**按 client_id 限速**，换一家就是换配额：
	// 实测 alist 约 2~20MB/s、oplist 约 0.5MB/s，差一个数量级。
	AliyunOpenRenewStyle string
	// AliyunOpenConnectorURL 是本地 TV token 连接器(小雅的 aliyuntvtoken_connector)。
	// oplist 的 TV 续期挂掉时作为回退。留空则不启用。
	// ⚠️ 它会把 refresh token 转发给第三方 api.extscreen.com(明文 HTTP)。
	AliyunOpenConnectorURL string
	AliyunTempFolderID     string // 转存目标临时目录的 file_id（小雅的 temp_transfer_folder_id）
	AliyunDriveID          string // 自己盘 drive_id，留空则从 web token 自动获取

	// 阿里各接口的基础域名（只抽域名，端点路径仍在代码里拼接）。
	// 阿里改域名（如 aliyundrive.com → alipan.com）时只改这里，不用改代码。
	AliyunAPIBase   string // 如 https://api.alipan.com
	AliyunAuthBase  string // 如 https://auth.alipan.com
	AliyunOpenBase  string // 如 https://openapi.alipan.com
	AliyunUserBase  string // 如 https://user.alipan.com
	AliyunBrowserUA string // 请求在线 token 服务时伪装的浏览器 UA

	// 分享目录列表缓存秒数（减少重复列目录的阿里调用，降低限流风险；0=关闭）
	AliyunListCacheSeconds int

	// HLSAllowedHosts 是 HLS 代理允许转发的上游主机白名单（防开放代理）。
	HLSAllowedHosts []string

	// 日志
	LogLevel  string // debug / info / warn / error
	LogFormat string // text / json

	// VideoExts 认定为视频的扩展名（小写，含点），用于过滤目录项。
	VideoExts []string
}

// JellyfinEnabled 表示是否配置了 Jellyfin（未配 API Key 则视为关闭）。
func (c Config) JellyfinEnabled() bool {
	return c.JellyfinBaseURL != "" && c.JellyfinAPIKey != ""
}

// 优先级：默认值 < 配置文件 < 环境变量。
//
// 嵌套配置键通过 viper 的 "." → "_" 映射自动对应环境变量：
// 例如 openlist.base_url 对应 OPENLIST_BASE_URL —— 现有 docker 环境变量全部继续生效。
func newViper(cfgFile string) (*viper.Viper, error) {
	v := viper.New()
	setDefaults(v)

	// 环境变量覆盖（AutomaticEnv 只对已注册的键生效，setDefaults 已注册全部键）。
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("conf")
		v.SetConfigType("yaml")
		v.AddConfigPath("./configs")
		v.AddConfigPath(".")
	}
	// 配置文件可选：不存在不报错，只有解析出错才报错。
	if err := v.ReadInConfig(); err != nil {
		var nf viper.ConfigFileNotFoundError
		if !errors.As(err, &nf) {
			return nil, err
		}
	}
	return v, nil
}

// setDefaults 注册所有配置键及默认值。
func setDefaults(v *viper.Viper) {
	v.SetDefault("server.port", "3001")
	v.SetDefault("server.api_key", "")

	v.SetDefault("openlist.base_url", "http://openlist:5244")
	v.SetDefault("openlist.username", "admin")
	v.SetDefault("openlist.password", "")
	v.SetDefault("openlist.root", "/")

	v.SetDefault("jellyfin.base_url", "")
	v.SetDefault("jellyfin.api_key", "")

	v.SetDefault("media_dir", "/media")
	v.SetDefault("site_url", "http://localhost:8090")
	v.SetDefault("strm.mode", "hls")
	v.SetDefault("stream.original_max_mbps", 3.5)
	v.SetDefault("strm.auto_interval_minutes", 30)

	v.SetDefault("db.driver", "mysql")
	v.SetDefault("db.dsn", "")
	v.SetDefault("db_path", "./ivideo.db") // sqlite 仅用于显式本地开发
	v.SetDefault("cache.backend", "fake")
	v.SetDefault("cache.dir", "/ivideo-cache")
	v.SetDefault("cache.max_bytes", int64(200*1024*1024*1024)) // 200 GB
	v.SetDefault("cache.ttl_hours", 72)
	v.SetDefault("cache.clean_interval_minutes", 10)
	v.SetDefault("cache.stop_grace_minutes", 10)
	v.SetDefault("cache.token_refresh_minutes", 30)
	v.SetDefault("share_check_interval_minutes", 15)
	v.SetDefault("discovery.concurrency", 4)
	v.SetDefault("discovery.timeout_seconds", 18)
	v.SetDefault("discovery.quick_wait_seconds", 4)
	v.SetDefault("discovery.cache_minutes", 10)
	v.SetDefault("discovery.max_results", 200)
	v.SetDefault("discovery.telegram_channels", []string{"tgsearchers3", "share_aliyun", "Quark_Movies", "Lsp115"})

	v.SetDefault("aliyun.list_cache_seconds", 60)

	v.SetDefault("import.max_depth", 8)
	v.SetDefault("import.max_files", 2000)

	v.SetDefault("aliyun.refresh_token", "")
	v.SetDefault("aliyun.open_refresh_token", "")
	v.SetDefault("aliyun.open_client_id", "")
	v.SetDefault("aliyun.open_client_secret", "")
	v.SetDefault("aliyun.open_token_url", "https://openapi.alipan.com/oauth/access_token")
	v.SetDefault("aliyun.open_renew_url", "https://api.alistgo.com/alist/ali_open/token")
	v.SetDefault("aliyun.open_renew_style", "alist")
	v.SetDefault("aliyun.open_connector_url", "")
	v.SetDefault("aliyun.temp_folder_id", "root")
	v.SetDefault("aliyun.drive_id", "")
	v.SetDefault("aliyun.api_base", "https://api.alipan.com")
	v.SetDefault("aliyun.auth_base", "https://auth.alipan.com")
	v.SetDefault("aliyun.open_base", "https://openapi.alipan.com")
	v.SetDefault("aliyun.user_base", "https://user.alipan.com")
	v.SetDefault("aliyun.browser_ua", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	v.SetDefault("hls.allowed_hosts", []string{
		"aliyundrive.com", "aliyundrive.net", "aliyundrive.cloud",
		"alipan.com", "aliyuncs.com", "alicdn.com",
	})

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")
}

// Load 按「默认值 → 配置文件 → 环境变量」加载配置。cfgFile 为空则查找 YAML 配置文件。
func Load(cfgFile string) (Config, error) {
	v, err := newViper(cfgFile)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:             v.GetString("server.port"),
		APIKey:           strings.TrimSpace(v.GetString("server.api_key")),
		OpenListBaseURL:  strings.TrimRight(v.GetString("openlist.base_url"), "/"),
		OpenListUsername: v.GetString("openlist.username"),
		OpenListPassword: v.GetString("openlist.password"),
		OpenListRoot:     v.GetString("openlist.root"),
		JellyfinBaseURL:  strings.TrimRight(v.GetString("jellyfin.base_url"), "/"),
		JellyfinAPIKey:   v.GetString("jellyfin.api_key"),

		MediaDir:         v.GetString("media_dir"),
		SiteURL:          strings.TrimRight(v.GetString("site_url"), "/"),
		StrmMode:         v.GetString("strm.mode"),
		OriginalMaxMbps:  v.GetFloat64("stream.original_max_mbps"),
		StrmAutoInterval: v.GetInt("strm.auto_interval_minutes"),

		DBDriver:                  v.GetString("db.driver"),
		DBDSN:                     v.GetString("db.dsn"),
		DBPath:                    v.GetString("db_path"),
		CacheBackend:              v.GetString("cache.backend"),
		CacheDir:                  v.GetString("cache.dir"),
		CacheMaxBytes:             v.GetInt64("cache.max_bytes"),
		CacheTTLHours:             v.GetInt("cache.ttl_hours"),
		CacheStopGrace:            v.GetInt("cache.stop_grace_minutes"),
		TokenRefreshMin:           v.GetInt("cache.token_refresh_minutes"),
		ShareCheckInterval:        v.GetInt("share_check_interval_minutes"),
		DiscoveryConcurrency:      v.GetInt("discovery.concurrency"),
		DiscoveryTimeoutSeconds:   v.GetInt("discovery.timeout_seconds"),
		DiscoveryQuickWaitSeconds: v.GetInt("discovery.quick_wait_seconds"),
		DiscoveryCacheMinutes:     v.GetInt("discovery.cache_minutes"),
		DiscoveryMaxResults:       v.GetInt("discovery.max_results"),
		DiscoveryTelegramChannels: v.GetStringSlice("discovery.telegram_channels"),
		AliyunListCacheSeconds:    v.GetInt("aliyun.list_cache_seconds"),
		ImportMaxDepth:            v.GetInt("import.max_depth"),
		ImportMaxFiles:            v.GetInt("import.max_files"),
		CacheCleanInterval:        v.GetInt("cache.clean_interval_minutes"),

		AliyunRefreshToken:     v.GetString("aliyun.refresh_token"),
		AliyunOpenRefreshToken: v.GetString("aliyun.open_refresh_token"),
		AliyunOpenClientID:     v.GetString("aliyun.open_client_id"),
		AliyunOpenClientSecret: v.GetString("aliyun.open_client_secret"),
		AliyunOpenTokenURL:     v.GetString("aliyun.open_token_url"),
		AliyunOpenRenewURL:     v.GetString("aliyun.open_renew_url"),
		AliyunOpenRenewStyle:   v.GetString("aliyun.open_renew_style"),
		AliyunOpenConnectorURL: v.GetString("aliyun.open_connector_url"),
		AliyunTempFolderID:     v.GetString("aliyun.temp_folder_id"),
		AliyunDriveID:          v.GetString("aliyun.drive_id"),
		AliyunAPIBase:          strings.TrimRight(v.GetString("aliyun.api_base"), "/"),
		AliyunAuthBase:         strings.TrimRight(v.GetString("aliyun.auth_base"), "/"),
		AliyunOpenBase:         strings.TrimRight(v.GetString("aliyun.open_base"), "/"),
		AliyunUserBase:         strings.TrimRight(v.GetString("aliyun.user_base"), "/"),
		AliyunBrowserUA:        v.GetString("aliyun.browser_ua"),

		HLSAllowedHosts: v.GetStringSlice("hls.allowed_hosts"),

		LogLevel:  v.GetString("log.level"),
		LogFormat: v.GetString("log.format"),

		VideoExts: []string{".mp4", ".mkv", ".webm", ".mov", ".avi", ".flv", ".m4v", ".ts"},
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate 在启动前拒绝不完整或不受支持的关键配置，避免悄悄连接到错误的数据源。
func (c Config) Validate() error {
	switch c.DBDriver {
	case "mysql":
		if strings.TrimSpace(c.DBDSN) == "" {
			return fmt.Errorf("db.dsn 不能为空（MySQL 为默认数据库；可通过 DB_DSN 环境变量设置）")
		}
	case "sqlite":
		if strings.TrimSpace(c.DBPath) == "" {
			return fmt.Errorf("db_path 不能为空（SQLite 本地开发模式）")
		}
	default:
		return fmt.Errorf("不支持的 db.driver %q（支持 mysql / sqlite）", c.DBDriver)
	}

	if c.StrmMode != "hls" && c.StrmMode != "original" {
		return fmt.Errorf("不支持的 strm.mode %q（支持 hls / original）", c.StrmMode)
	}
	if strings.TrimSpace(c.SiteURL) == "" {
		return fmt.Errorf("site_url 不能为空")
	}
	return nil
}
