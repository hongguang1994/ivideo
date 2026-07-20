package cmd

import (
	"fmt"
	"log"

	"ivideo/server/internal/app"
	"ivideo/server/internal/config"
	"ivideo/server/internal/store"
)

// runServer 加载配置、打开数据库、组装路由并启动 HTTP 服务。
func runServer() error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer st.Close()
	seedIfEmpty(st)

	r, err := app.New(cfg, st)
	if err != nil {
		return fmt.Errorf("初始化失败: %w", err)
	}

	addr := ":" + cfg.Port
	log.Printf("ivideo 后端启动，监听 %s，OpenList=%s 根目录=%s",
		addr, cfg.OpenListBaseURL, cfg.OpenListRoot)
	return r.Run(addr)
}

// seedIfEmpty 在资源目录为空时写入几条示例，便于首次联调。
func seedIfEmpty(st store.Store) {
	n, err := st.CountResources()
	if err != nil || n > 0 {
		return
	}
	samples := []store.Resource{
		{Title: "示例影片 A（阿里分享）", Provider: "aliyun", ShareURL: "https://www.alipan.com/s/example-a", Overview: "用于联调的示例资源"},
		{Title: "示例影片 B（阿里分享）", Provider: "aliyun", ShareURL: "https://www.alipan.com/s/example-b", Overview: "用于联调的示例资源"},
	}
	for _, s := range samples {
		if _, err := st.AddResource(s); err != nil {
			log.Printf("seed 失败: %v", err)
		}
	}
	log.Printf("已写入 %d 条示例资源", len(samples))
}
