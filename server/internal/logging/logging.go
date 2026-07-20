// Package logging 配置全局 slog 结构化日志。
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Setup 按配置初始化全局 slog 默认 logger。
// level: debug/info/warn/error（默认 info）；format: text/json（默认 text）。
func Setup(level, format string) {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var h slog.Handler
	if strings.EqualFold(format, "json") {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(h))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
