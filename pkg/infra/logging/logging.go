// Package logging builds the loggers of the monitor.
//
// The daemon writes two streams: a machine oriented one on the standard
// output, whose format the operator chooses, and a plain text file with one
// human readable line per event, which is the log the operator reads when
// checking that the monitor is doing its job.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Options configure the logger.
type Options struct {
	// Level is the minimum severity written to the standard output.
	Level slog.Level
	// Format is "text" or "json" and applies to the standard output only.
	Format string
	// FilePath is where the plain text log file is written. When it is
	// empty no file is written.
	FilePath string
	// FileLevel is the minimum severity written to the log file.
	FileLevel slog.Level
}

// New builds the logger and the closer of the resources it holds.
//
// The returned closer releases the log file. It is never nil, even when no
// file is written.
func New(options Options) (*slog.Logger, io.Closer, error) {
	var handlers []slog.Handler

	if options.Format == FormatJSON {
		handlers = append(handlers, slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: options.Level}))
	} else {
		handlers = append(handlers, slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: options.Level}))
	}

	var file *os.File
	if strings.TrimSpace(options.FilePath) != "" {
		opened, err := Open(options.FilePath)
		if err != nil {
			return nil, nil, err
		}
		file = opened
		handlers = append(handlers, NewPlainHandler(file, options.FileLevel))
	}

	return slog.New(fanout(handlers)), closer{file: file}, nil
}

// The formats accepted by the standard output stream.
const (
	// FormatText writes the key and value pairs of every event.
	FormatText = "text"
	// FormatJSON writes one JSON object per event.
	FormatJSON = "json"
)

// Open opens the plain text log file for appending. The directory of the
// file is created when it does not exist yet.
func Open(path string) (*os.File, error) {
	directory := filepath.Dir(path)
	if directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return nil, fmt.Errorf("create the directory of the log file %s: %w", path, err)
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the log file %s: %w", path, err)
	}
	return file, nil
}

// closer releases the log file, if any.
type closer struct {
	file *os.File
}

// Close implements io.Closer.
func (c closer) Close() error {
	if c.file == nil {
		return nil
	}
	return c.file.Close()
}

// fanout sends every record to several handlers.
type fanout []slog.Handler

// Enabled implements slog.Handler.
func (f fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range f {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle implements slog.Handler.
func (f fanout) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for _, handler := range f {
		if !handler.Enabled(ctx, record.Level) {
			continue
		}
		if err := handler.Handle(ctx, record.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// WithAttrs implements slog.Handler.
func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make(fanout, 0, len(f))
	for _, handler := range f {
		next = append(next, handler.WithAttrs(attrs))
	}
	return next
}

// WithGroup implements slog.Handler.
func (f fanout) WithGroup(name string) slog.Handler {
	next := make(fanout, 0, len(f))
	for _, handler := range f {
		next = append(next, handler.WithGroup(name))
	}
	return next
}
