package app_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gnrfan/dns-change-monitor/pkg/app"
	"github.com/gnrfan/dns-change-monitor/pkg/config"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/notification"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/subscriber"
	domaintelegram "github.com/gnrfan/dns-change-monitor/pkg/domain/telegram"
	"github.com/gnrfan/dns-change-monitor/pkg/testsupport"
)

// gateway is a Telegram gateway driven by the test.
type gateway struct {
	commands chan domaintelegram.Command
}

// newGateway builds a gateway.
func newGateway() *gateway {
	return &gateway{commands: make(chan domaintelegram.Command, 8)}
}

// Listen implements telegram.Gateway.
func (g *gateway) Listen(ctx context.Context) (<-chan domaintelegram.Command, error) {
	outgoing := make(chan domaintelegram.Command, 8)
	go func() {
		defer close(outgoing)
		for {
			select {
			case <-ctx.Done():
				return
			case command := <-g.commands:
				select {
				case outgoing <- command:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return outgoing, nil
}

func writeConfiguration(t *testing.T, directory string) string {
	t.Helper()
	content := `
[database]
path = "` + filepath.Join(directory, "dnsmon.db") + `"

[telegram]
token = "123456:test"
notify_usernames = ["alice"]

[monitor]
interval_seconds = 1
resolvers = ["127.0.0.1:5353"]

[[monitor.records]]
name = "example.com"
type = "A"
`
	path := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return path
}

func TestMonitorReportsALiveChangeToAPairedAccount(t *testing.T) {
	directory := t.TempDir()
	cfg, err := config.Load(writeConfiguration(t, directory))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	application, err := app.Open(cfg, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer application.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := application.Seed(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	targets, err := application.Repositories().Targets.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 || targets[0].Name != "example.com" {
		t.Fatalf("unexpected targets: %+v", targets)
	}

	subscribers, err := application.Repositories().Subscribers.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(subscribers) != 1 || subscribers[0].Username != "alice" {
		t.Fatalf("unexpected subscribers: %+v", subscribers)
	}
	if subscribers[0].Status != subscriber.StatusPending {
		t.Fatalf("the subscriber must wait for a pairing: %q", subscribers[0].Status)
	}

	resolver := testsupport.NewResolver()
	resolver.SetAnswer(targets[0].Identity(), "203.0.113.10")
	notifier := testsupport.NewNotifier()
	commands := newGateway()

	application.SetResolver(resolver)
	application.SetGateway(commands, notifier)
	if err := application.Assemble(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, pairingService := application.Services()
	token, err := pairingService.Issue(ctx, time.Minute, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	go func() { _ = application.Run(runCtx) }()

	commands.commands <- domaintelegram.Command{
		ChatID:   -1001,
		UserID:   42,
		Username: "alice",
		Command:  "start",
		Payload:  token.ID,
	}

	waitFor(t, 5*time.Second, func() bool {
		entry, err := application.Repositories().Subscribers.FindByUsername(ctx, "alice")
		return err == nil && entry.IsPaired()
	}, "the account must be paired")

	// The answer of the name changes, the next sweep must report it.
	resolver.SetAnswer(targets[0].Identity(), "203.0.113.11")

	waitFor(t, 10*time.Second, func() bool {
		return reportReceived(notifier, "203.0.113.11")
	}, "the change must be reported")
}

// reportReceived reports whether a message mentioning the value was sent.
func reportReceived(notifier *testsupport.Notifier, value string) bool {
	for _, message := range notifier.Messages {
		if contains(message.Text, value) {
			return true
		}
	}
	return false
}

// waitFor polls a condition until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(message)
}

func contains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Compile time check that the test doubles satisfy the domain ports.
var (
	_ domaintelegram.Gateway = (*gateway)(nil)
	_ notification.Notifier  = (*testsupport.Notifier)(nil)
)
