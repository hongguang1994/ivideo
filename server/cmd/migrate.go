package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"ivideo/server/internal/store"
)

var (
	migFromDriver string
	migFromDSN    string
	migToDriver   string
	migToDSN      string
)

// migrateCmd 把数据从一个库迁移到另一个(如 SQLite → MySQL),保留资源 ID。
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "迁移数据到另一个数据库(如 SQLite → MySQL)",
	Long: "把源库全部数据(resources / cache_items / credentials)复制到目标库。\n" +
		"目标应为空库。示例:\n" +
		"  ivideo migrate --from /data/ivideo.db \\\n" +
		"    --to-driver mysql --to 'root:pass@tcp(mysql:3306)/ivideo?charset=utf8mb4'",
	RunE: func(cmd *cobra.Command, args []string) error {
		counts, err := store.Migrate(migFromDriver, migFromDSN, migToDriver, migToDSN)
		for t, n := range counts {
			fmt.Printf("  %-12s 迁移 %d 行\n", t, n)
		}
		if err != nil {
			return err
		}
		fmt.Println("✅ 迁移完成")
		return nil
	},
}

func init() {
	migrateCmd.Flags().StringVar(&migFromDriver, "from-driver", "sqlite", "源驱动 sqlite / mysql")
	migrateCmd.Flags().StringVar(&migFromDSN, "from", "", "源 DSN(sqlite 填文件路径)")
	migrateCmd.Flags().StringVar(&migToDriver, "to-driver", "mysql", "目标驱动 sqlite / mysql")
	migrateCmd.Flags().StringVar(&migToDSN, "to", "", "目标 DSN")
	_ = migrateCmd.MarkFlagRequired("from")
	_ = migrateCmd.MarkFlagRequired("to")
	rootCmd.AddCommand(migrateCmd)
}
