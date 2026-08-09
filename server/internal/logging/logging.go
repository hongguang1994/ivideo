// Package logging 配置全局 slog 结构化日志。
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// Entry 是推送给管理界面的结构化日志。
type Entry struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// Hub 保存最近日志并向 WebSocket 订阅者广播。
type Hub struct {
	mu      sync.RWMutex
	history []Entry
	clients map[chan Entry]struct{}
	limit   int
}

var defaultHub = NewHub(500)

func NewHub(limit int) *Hub {
	if limit <= 0 {
		limit = 500
	}
	return &Hub{limit: limit, clients: make(map[chan Entry]struct{})}
}

func DefaultHub() *Hub { return defaultHub }

// Subscribe 返回当前历史快照和后续日志通道。
func (h *Hub) Subscribe() ([]Entry, <-chan Entry, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	history := append([]Entry(nil), h.history...)
	stream := make(chan Entry, 128)
	h.clients[stream] = struct{}{}
	return history, stream, func() {
		h.mu.Lock()
		if _, ok := h.clients[stream]; ok {
			delete(h.clients, stream)
			close(stream)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) publish(entry Entry) {
	h.mu.Lock()
	h.history = append(h.history, entry)
	if len(h.history) > h.limit {
		h.history = append([]Entry(nil), h.history[len(h.history)-h.limit:]...)
	}
	for stream := range h.clients {
		select {
		case stream <- entry:
		default:
			// 慢客户端不应阻塞业务日志。
		}
	}
	h.mu.Unlock()
}

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
	slog.SetDefault(slog.New(&broadcastHandler{next: h, hub: defaultHub}))
}

type broadcastHandler struct {
	next   slog.Handler
	hub    *Hub
	bound  map[string]any
	groups []string
}

func (h *broadcastHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *broadcastHandler) Handle(ctx context.Context, record slog.Record) error {
	if err := h.next.Handle(ctx, record); err != nil {
		return err
	}
	attrs := make(map[string]any)
	for key, value := range h.bound {
		attrs[key] = value
	}
	record.Attrs(func(attr slog.Attr) bool {
		addAttr(attrs, h.groups, attr)
		return true
	})
	loggedAt := record.Time
	if loggedAt.IsZero() {
		loggedAt = time.Now()
	}
	h.hub.publish(Entry{Time: loggedAt, Level: strings.ToLower(record.Level.String()), Message: record.Message, Attrs: attrs})
	return nil
}

func (h *broadcastHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	bound := make(map[string]any, len(h.bound)+len(attrs))
	for key, value := range h.bound {
		bound[key] = value
	}
	for _, attr := range attrs {
		addAttr(bound, h.groups, attr)
	}
	return &broadcastHandler{next: h.next.WithAttrs(attrs), hub: h.hub, bound: bound, groups: append([]string(nil), h.groups...)}
}

func (h *broadcastHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	groups := append(append([]string(nil), h.groups...), name)
	bound := make(map[string]any, len(h.bound))
	for key, value := range h.bound {
		bound[key] = value
	}
	return &broadcastHandler{next: h.next.WithGroup(name), hub: h.hub, bound: bound, groups: groups}
}

func addAttr(target map[string]any, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	key := strings.Join(append(groups, attr.Key), ".")
	if sensitiveKey(key) {
		target[key] = "[REDACTED]"
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		for _, child := range attr.Value.Group() {
			addAttr(target, append(groups, attr.Key), child)
		}
		return
	}
	target[key] = logValue(attr.Value)
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, part := range []string{"token", "cookie", "secret", "password", "passwd", "authorization"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

func logValue(value slog.Value) any {
	switch value.Kind() {
	case slog.KindString:
		return value.String()
	case slog.KindBool:
		return value.Bool()
	case slog.KindInt64:
		return value.Int64()
	case slog.KindUint64:
		return value.Uint64()
	case slog.KindFloat64:
		return value.Float64()
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().Format(time.RFC3339Nano)
	case slog.KindAny:
		if err, ok := value.Any().(error); ok {
			return err.Error()
		}
		return fmt.Sprint(value.Any())
	default:
		return value.String()
	}
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
