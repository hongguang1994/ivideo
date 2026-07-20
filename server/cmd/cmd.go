package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// cfgFile 是 --config 指定的配置文件路径（空则自动查找 ./conf/conf.json）。
var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "ivideo",
	Short: "ivideo 后端服务",
	Long:  "ivideo 网盘视频平台后端。不带子命令时默认启动 HTTP 服务。",
	// 默认动作：启动服务（这样容器 ENTRYPOINT 不用改）。
	RunE: func(cmd *cobra.Command, args []string) error {
		return runServer()
	},
	SilenceUsage: true, // 运行期出错时不打印一大段 usage
}

// Execute 是程序入口。
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "配置文件路径（默认查找 ./conf/conf.json）")
}
