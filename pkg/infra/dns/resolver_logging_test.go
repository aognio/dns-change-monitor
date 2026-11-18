package dns_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/domain/record"
	"github.com/aognio/dns-change-monitor/pkg/infra/dns"
	"github.com/aognio/dns-change-monitor/pkg/infra/logging"
)

func TestResolverRecordsTheQueriesItSends(t *testing.T) {
	address := startServer(t, map[string]string{
		"example.com.": "example.com. 60 IN A 203.0.113.10",
	})

	var buffer bytes.Buffer
	logger := slog.New(logging.NewPlainHandler(&buffer, slog.LevelInfo))

	resolver := dns.New(dns.Options{
		Servers: []string{address},
		Timeout: 2 * time.Second,
		Logger:  logger,
	})
	target, err := record.NewTarget("example.com", record.TypeA, "", record.SourceManual, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := resolver.Resolve(context.Background(), target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buffer.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("got %d lines, want the query and the answer: %q", len(lines), buffer.String())
	}

	query := lines[0]
	if !strings.Contains(query, "dns query") {
		t.Fatalf("the line %q does not record the query", query)
	}
	if !strings.Contains(query, "name=example.com") {
		t.Fatalf("the line %q does not name the queried name", query)
	}
	if !strings.Contains(query, "type=A") {
		t.Fatalf("the line %q does not name the queried type", query)
	}
	if !strings.Contains(query, "server="+address) {
		t.Fatalf("the line %q does not name the queried server", query)
	}

	answer := lines[1]
	if !strings.Contains(answer, "dns answer") {
		t.Fatalf("the line %q does not record the answer", answer)
	}
	if !strings.Contains(answer, "answers=1") {
		t.Fatalf("the line %q does not count the answers", answer)
	}
}
