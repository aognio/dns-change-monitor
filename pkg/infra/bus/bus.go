// Package bus provides an in-process implementation of the domain event bus.
package bus

import (
	"context"
	"log/slog"
	"sync"

	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
)

// MemoryBus delivers events to the handlers registered for their name.
// Handlers run in their own goroutine so that a slow subscriber never blocks
// the publisher.
type MemoryBus struct {
	mutex    sync.RWMutex
	handlers map[string][]shared.EventHandler
	logger   *slog.Logger
}

// New builds an empty bus.
func New(logger *slog.Logger) *MemoryBus {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryBus{
		handlers: make(map[string][]shared.EventHandler),
		logger:   logger,
	}
}

// Subscribe implements shared.EventBus.
func (b *MemoryBus) Subscribe(eventName string, handler shared.EventHandler) {
	if handler == nil {
		return
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.handlers[eventName] = append(b.handlers[eventName], handler)
}

// Publish implements shared.EventBus.
func (b *MemoryBus) Publish(ctx context.Context, event shared.Event) error {
	if event == nil {
		return nil
	}
	b.mutex.RLock()
	handlers := append([]shared.EventHandler(nil), b.handlers[event.Name()]...)
	b.mutex.RUnlock()

	for _, handler := range handlers {
		go b.dispatch(ctx, event, handler)
	}
	return nil
}

// dispatch runs a handler and turns a panic into a logged error.
func (b *MemoryBus) dispatch(ctx context.Context, event shared.Event, handler shared.EventHandler) {
	defer func() {
		if recovered := recover(); recovered != nil {
			b.logger.Error("event handler panicked",
				slog.String("event", event.Name()),
				slog.Any("panic", recovered))
		}
	}()

	if err := handler(ctx, event); err != nil {
		b.logger.Error("event handler failed",
			slog.String("event", event.Name()),
			slog.String("error", err.Error()))
	}
}
