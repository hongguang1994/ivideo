package eventbus

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type testEvent struct{ value string }

func (testEvent) Name() string { return "test.event" }

func TestMemoryBusDeliversAndUnsubscribes(t *testing.T) {
	bus := New()
	var values []string
	unsubscribe := bus.Subscribe("test.event", func(_ context.Context, event Event) error {
		values = append(values, event.(testEvent).value)
		return nil
	})
	if err := bus.Publish(context.Background(), testEvent{value: "first"}); err != nil {
		t.Fatal(err)
	}
	unsubscribe()
	if err := bus.Publish(context.Background(), testEvent{value: "second"}); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0] != "first" {
		t.Fatalf("unexpected deliveries: %#v", values)
	}
}

func TestMemoryBusReturnsSubscriberErrors(t *testing.T) {
	bus := New()
	bus.Subscribe("test.event", func(context.Context, Event) error { return errors.New("failed") })
	err := bus.Publish(context.Background(), testEvent{})
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected subscriber error, got %v", err)
	}
}

func TestMemoryBusContainsSubscriberPanics(t *testing.T) {
	bus := New()
	bus.Subscribe("test.event", func(context.Context, Event) error { panic("broken plugin") })
	err := bus.Publish(context.Background(), testEvent{})
	if err == nil || !strings.Contains(err.Error(), "broken plugin") {
		t.Fatalf("expected contained panic, got %v", err)
	}
}
