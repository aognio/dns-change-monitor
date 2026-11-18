package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// plainHandler writes one human readable line per record.
type plainHandler struct {
	writer io.Writer
	level  slog.Level
	mutex  *sync.Mutex
	attrs  []slog.Attr
	groups []string
}

// NewPlainHandler builds a handler that writes one plain text line per
// record.
//
// The lines look like this:
//
//	2026-11-18T17:04:05Z INFO dns query name=example.com type=A server=1.1.1.1:53
func NewPlainHandler(writer io.Writer, level slog.Level) slog.Handler {
	return &plainHandler{
		writer: writer,
		level:  level,
		mutex:  &sync.Mutex{},
	}
}

// Enabled implements slog.Handler.
func (h *plainHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle implements slog.Handler.
func (h *plainHandler) Handle(_ context.Context, record slog.Record) error {
	var builder strings.Builder

	timestamp := record.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	builder.WriteString(timestamp.UTC().Format("2006-01-02T15:04:05Z"))
	builder.WriteString(" ")
	builder.WriteString(strings.ToUpper(record.Level.String()))
	builder.WriteString(" ")
	builder.WriteString(record.Message)

	for _, attr := range h.attrs {
		h.writeAttr(&builder, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		h.writeAttr(&builder, attr)
		return true
	})

	builder.WriteString("\n")

	h.mutex.Lock()
	defer h.mutex.Unlock()
	_, err := io.WriteString(h.writer, builder.String())
	return err
}

// WithAttrs implements slog.Handler.
func (h *plainHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &next
}

// WithGroup implements slog.Handler.
func (h *plainHandler) WithGroup(name string) slog.Handler {
	next := *h
	next.groups = append(append([]string(nil), h.groups...), name)
	return &next
}

// writeAttr renders one attribute as a key and value pair.
func (h *plainHandler) writeAttr(builder *strings.Builder, attr slog.Attr) {
	if attr.Equal(slog.Attr{}) {
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		group := attr.Value.Group()
		if len(group) == 0 {
			return
		}
		for _, member := range group {
			h.writeAttr(builder, member)
		}
		return
	}

	key := attr.Key
	if len(h.groups) > 0 {
		key = strings.Join(h.groups, ".") + "." + key
	}
	builder.WriteString(" ")
	builder.WriteString(key)
	builder.WriteString("=")
	builder.WriteString(plainValue(attr.Value))
}

// plainValue renders a value without the quoting of the standard formats
// when it does not need it.
func plainValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		text := value.String()
		if text == "" || strings.ContainsAny(text, " \t\"=") {
			return strconv.Quote(text)
		}
		return text
	case slog.KindTime:
		return value.Time().UTC().Format(time.RFC3339)
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindAny:
		if err, ok := value.Any().(error); ok {
			return strconv.Quote(err.Error())
		}
		return fmt.Sprintf("%v", value.Any())
	default:
		return value.String()
	}
}
