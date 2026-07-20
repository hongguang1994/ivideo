package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ivideo/server/internal/app"
	"ivideo/server/internal/config"
	"ivideo/server/internal/logging"
	"ivideo/server/internal/store"
)

// runServer 加载配置、打开数据库、组装路由并启动 HTTP 服务（支持优雅关闭）。
func runServer() error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	logging.Setup(cfg.LogLevel, cfg.LogFormat)

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
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
		// 只限「读请求头」，防慢连接；不设 Read/WriteTimeout —— 否则视频流会被掐断。
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("ivideo 后端启动", "addr", addr, "openlist", cfg.OpenListBaseURL, "root", cfg.OpenListRoot)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("监听失败", "err", err)
			os.Exit(1)
		}
	}()

	// 等待中断信号，优雅关闭。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("正在关闭 ivideo…")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("服务关闭失败: %w", err)
	}
	slog.Info("ivideo 已停止")
	return nil
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
			slog.Error("seed 失败", "err", err)
		}
	}
	slog.Info("已写入示例资源", "count", len(samples))
}
