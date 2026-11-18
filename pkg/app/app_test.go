package app_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/app"
	"github.com/aognio/dns-change-monitor/pkg/config"
	"github.com/aognio/dns-change-monitor/pkg/domain/notification"
	"github.com/aognio/dns-change-monitor/pkg/domain/record"
	"github.com/aognio/dns-change-monitor/pkg/domain/subscriber"
	domaintelegram "github.com/aognio/dns-change-monitor/pkg/domain/telegram"
	"github.com/aognio/dns-change-monitor/pkg/testsupport"
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

// writeCheckedConfiguration writes a configuration file declaring one record
// with the given check mode and expectation.
func writeCheckedConfiguration(t *testing.T, directory string, mode string, expect []string) string {
	t.Helper()
	expectation := ""
	if len(expect) > 0 {
		values := make([]string, 0, len(expect))
		for _, value := range expect {
			values = append(values, `"`+value+`"`)
		}
		expectation = "\nexpect = [" + strings.Join(values, ", ") + "]"
	}
	content := `
[database]
path = "` + filepath.Join(directory, "dnsmon.db") + `"

[telegram]
token = "123456:test"

[monitor]
interval_seconds = 1
resolvers = ["127.0.0.1:5353"]

[[monitor.records]]
name = "example.com"
type = "A"
mode = "` + mode + `"` + expectation + `
`
	path := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return path
}

func openApplication(t *testing.T, configPath string) *app.App {
	t.Helper()
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	application, err := app.Open(cfg, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	return application
}

func TestSeedAppliesTheCheckDeclaredInTheConfigurationFile(t *testing.T) {
	directory := t.TempDir()
	application := openApplication(t, writeCheckedConfiguration(t, directory, "exact", []string{"1.2.3.4"}))

	ctx := context.Background()
	if err := application.Seed(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	targets, err := application.Repositories().Targets.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected one target, got %d", len(targets))
	}
	if targets[0].Check.Mode != record.ModeExact {
		t.Fatalf("unexpected mode: %q", targets[0].Check.Mode)
	}
	if len(targets[0].Check.Expect) != 1 || targets[0].Check.Expect[0] != "1.2.3.4" {
		t.Fatalf("unexpected expectation: %v", targets[0].Check.Expect)
	}
}

func TestSeedUpdatesTheModeOfAnExistingTarget(t *testing.T) {
	directory := t.TempDir()
	application := openApplication(t, writeCheckedConfiguration(t, directory, "exact", []string{"1.2.3.4"}))

	ctx := context.Background()
	if err := application.Seed(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	before, err := application.Repositories().Targets.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The configuration file changes the mode of the record.
	application2 := openApplication(t, writeCheckedConfiguration(t, directory, "any", nil))
	if err := application2.Seed(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	after, err := application2.Repositories().Targets.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("expected one target, got %d", len(after))
	}
	if after[0].ID != before[0].ID {
		t.Fatalf("the target must keep its identifier, got %q", after[0].ID)
	}
	if after[0].Check.Mode != record.ModeAny {
		t.Fatalf("unexpected mode: %q", after[0].Check.Mode)
	}
}

func TestCheckReportsTheVerdictOfTheExpectation(t *testing.T) {
	directory := t.TempDir()
	application := openApplication(t, writeCheckedConfiguration(t, directory, "exact", []string{"1.2.3.4"}))

	ctx := context.Background()
	if err := application.Seed(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	targets, err := application.Repositories().Targets.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolver := testsupport.NewResolver()
	resolver.SetAnswer(targets[0].Identity(), "5.6.7.8")
	application.SetResolver(resolver)

	results, err := application.Check(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if results[0].Satisfied {
		t.Fatal("the expectation is violated")
	}
	if results[0].Reason == "" {
		t.Fatal("the result must carry the reason of the verdict")
	}
	verdict := results[0].Verdict()
	if !strings.Contains(verdict, "exact violated") {
		t.Fatalf("unexpected verdict: %q", verdict)
	}
}

func TestCheckReportsASatisfiedExpectation(t *testing.T) {
	directory := t.TempDir()
	application := openApplication(t, writeCheckedConfiguration(t, directory, "exact", []string{"1.2.3.4"}))

	ctx := context.Background()
	if err := application.Seed(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	targets, err := application.Repositories().Targets.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolver := testsupport.NewResolver()
	resolver.SetAnswer(targets[0].Identity(), "1.2.3.4")
	application.SetResolver(resolver)

	results, err := application.Check(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[0].Satisfied {
		t.Fatal("the expectation is satisfied")
	}
	if !strings.Contains(results[0].Verdict(), "exact satisfied") {
		t.Fatalf("unexpected verdict: %q", results[0].Verdict())
	}
}
