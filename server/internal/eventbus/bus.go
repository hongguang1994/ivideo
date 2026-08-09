// Package eventbus provides synchronous in-process domain event delivery.
package eventbus

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

type Event interface {
	Name() string
}

type Handler func(context.Context, Event) error

type Publisher interface {
	Publish(context.Context, Event) error
}

type Bus interface {
	Publisher
	Subscribe(eventName string, handler Handler) func()
}

type MemoryBus struct {
	mu       sync.RWMutex
	nextID   uint64
	handlers map[string]map[uint64]Handler
}

func New() *MemoryBus {
	return &MemoryBus{handlers: make(map[string]map[uint64]Handler)}
}

func (b *MemoryBus) Subscribe(eventName string, handler Handler) func() {
	if b == nil || eventName == "" || handler == nil {
		return func() {}
	}
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	if b.handlers[eventName] == nil {
		b.handlers[eventName] = make(map[uint64]Handler)
	}
	b.handlers[eventName][id] = handler
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.handlers[eventName], id)
		if len(b.handlers[eventName]) == 0 {
			delete(b.handlers, eventName)
		}
		b.mu.Unlock()
	}
}

func (b *MemoryBus) Publish(ctx context.Context, event Event) error {
	if event == nil || event.Name() == "" {
		return errors.New("领域事件名称不能为空")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.RLock()
	registered := b.handlers[event.Name()]
	handlers := make([]Handler, 0, len(registered))
	ids := make([]uint64, 0, len(registered))
	for id := range registered {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		handlers = append(handlers, registered[id])
	}
	b.mu.RUnlock()

	var failures []error
	for _, handler := range handlers {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		if err := invoke(ctx, handler, event); err != nil {
			failures = append(failures, fmt.Errorf("处理事件 %s: %w", event.Name(), err))
		}
	}
	return errors.Join(failures...)
}

func invoke(ctx context.Context, handler Handler, event Event) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("订阅者异常: %v", recovered)
		}
	}()
	return handler(ctx, event)
}
