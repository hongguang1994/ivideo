package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"ivideo/server/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "配置相关命令",
}

var configCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "校验配置能否正确加载，并打印生效配置（敏感项脱敏）",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("配置加载失败: %w", err)
		}
		printConfig(cfg)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configCheckCmd)
	rootCmd.AddCommand(configCmd)
}

func printConfig(cfg config.Config) {
	mask := func(s string) string {
		if s == "" {
			return "(空)"
		}
		return "***已设置***"
	}
	fmt.Println("配置校验通过 ✓  生效配置如下：")
	fmt.Println("  server.port           =", cfg.Port)
	fmt.Println("  db_path               =", cfg.DBPath)
	fmt.Println("  site_url              =", cfg.SiteURL)
	fmt.Println("  media_dir             =", cfg.MediaDir)
	fmt.Println("  strm.mode             =", cfg.StrmMode)
	fmt.Println("  openlist.base_url     =", cfg.OpenListBaseURL)
	fmt.Println("  jellyfin.base_url     =", cfg.JellyfinBaseURL)
	fmt.Println("  cache.backend         =", cfg.CacheBackend)
	fmt.Println("  cache.max_bytes       =", cfg.CacheMaxBytes)
	fmt.Println("  cache.ttl_hours       =", cfg.CacheTTLHours)
	fmt.Println("  cache.clean_interval  =", cfg.CacheCleanInterval)
	fmt.Println("  aliyun.temp_folder_id =", cfg.AliyunTempFolderID)
	fmt.Println("  aliyun.drive_id       =", cfg.AliyunDriveID)
	fmt.Println("  aliyun.api_base       =", cfg.AliyunAPIBase)
	fmt.Println("  aliyun.auth_base      =", cfg.AliyunAuthBase)
	fmt.Println("  aliyun.open_base      =", cfg.AliyunOpenBase)
	fmt.Println("  aliyun.user_base      =", cfg.AliyunUserBase)
	fmt.Println("  aliyun.open_renew_url =", cfg.AliyunOpenRenewURL)
	fmt.Println("  aliyun.open_connector_url =", cfg.AliyunOpenConnectorURL)
	fmt.Println("  hls.allowed_hosts     =", cfg.HLSAllowedHosts)
	fmt.Println("  aliyun.refresh_token      =", mask(cfg.AliyunRefreshToken))
	fmt.Println("  aliyun.open_refresh_token =", mask(cfg.AliyunOpenRefreshToken))
	fmt.Println("  jellyfin.api_key          =", mask(cfg.JellyfinAPIKey))
}
