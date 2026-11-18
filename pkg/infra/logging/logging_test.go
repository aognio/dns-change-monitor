package logging_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/aognio/dns-change-monitor/pkg/infra/logging"
)

func TestPlainHandlerWritesOneLinePerRecord(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(logging.NewPlainHandler(&buffer, slog.LevelInfo))

	logger.Info("dns query",
		slog.String("name", "example.com"),
		slog.String("type", "A"),
		slog.String("server", "1.1.1.1:53"))

	lines := strings.Split(strings.TrimRight(buffer.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %q", len(lines), buffer.String())
	}

	line := lines[0]
	if !strings.Contains(line, "INFO") {
		t.Fatalf("the line %q does not carry the level", line)
	}
	if !strings.Contains(line, "dns query") {
		t.Fatalf("the line %q does not carry the message", line)
	}
	if !strings.Contains(line, "name=example.com") {
		t.Fatalf("the line %q does not carry the attributes", line)
	}
	if !strings.HasPrefix(line, "20") {
		t.Fatalf("the line %q does not start with a timestamp", line)
	}
}

func TestPlainHandlerHidesTheRecordsBelowTheLevel(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(logging.NewPlainHandler(&buffer, slog.LevelWarn))

	logger.Info("dns query", slog.String("name", "example.com"))
	if buffer.Len() != 0 {
		t.Fatalf("got %q, want nothing", buffer.String())
	}

	logger.Warn("dns query failed", slog.String("name", "example.com"))
	if !strings.Contains(buffer.String(), "dns query failed") {
		t.Fatalf("got %q, want the warning", buffer.String())
	}
}

func TestPlainHandlerQuotesTheValuesThatNeedIt(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(logging.NewPlainHandler(&buffer, slog.LevelInfo))

	logger.Info("dns query failed", slog.String("error", "no route to host"))

	if !strings.Contains(buffer.String(), `error="no route to host"`) {
		t.Fatalf("got %q, want a quoted error", buffer.String())
	}
}
