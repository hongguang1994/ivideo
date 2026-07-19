package main

import (
	"log"

	"ivideo/server/internal"
	"ivideo/server/internal/config"
)

func main() {
	cfg := config.Load()
	r := internal.NewRouter(cfg)

	addr := ":" + cfg.Port
	log.Printf("ivideo 后端启动，监听 %s，OpenList=%s 根目录=%s",
		addr, cfg.OpenListBaseURL, cfg.OpenListRoot)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
