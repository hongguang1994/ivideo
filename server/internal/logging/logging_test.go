package logging

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestHubKeepsRecentEntries(t *testing.T) {
	hub := NewHub(2)
	hub.publish(Entry{Message: "one"})
	hub.publish(Entry{Message: "two"})
	hub.publish(Entry{Message: "three"})
	history, _, unsubscribe := hub.Subscribe()
	unsubscribe()
	if len(history) != 2 || history[0].Message != "two" || history[1].Message != "three" {
		t.Fatalf("unexpected history: %+v", history)
	}
}

func TestBroadcastHandlerRedactsSensitiveAttrs(t *testing.T) {
	hub := NewHub(10)
	handler := &broadcastHandler{next: slog.NewTextHandler(discardWriter{}, nil), hub: hub}
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	record.Add("token", "secret-value", "path", "/media")
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	history, _, unsubscribe := hub.Subscribe()
	unsubscribe()
	if got := history[0].Attrs["token"]; got != "[REDACTED]" {
		t.Fatalf("token was not redacted: %v", got)
	}
	if got := history[0].Attrs["path"]; got != "/media" {
		t.Fatalf("ordinary attr changed: %v", got)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
