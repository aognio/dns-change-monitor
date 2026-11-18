package pairer_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/agent/pairer"
	apppairing "github.com/aognio/dns-change-monitor/pkg/app/pairing"
	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
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

// waitMessage waits for a reply to be delivered.
func waitMessage(t *testing.T, notifier *testsupport.Notifier) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if notifier.Delivered() > 0 {
			return notifier.Messages[0].Text
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no reply was delivered")
	return ""
}

// newAgent builds a pairing agent wired to the test doubles.
func newAgent(t *testing.T, gateway *gateway, notifier *testsupport.Notifier) *pairer.Agent {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	service := apppairing.New(
		testsupport.NewTokens(),
		testsupport.NewSubscribers(),
		nil,
		shared.UUIDGenerator{},
		shared.SystemClock{},
		logger,
	)
	return pairer.New(pairer.Options{
		Gateway:  gateway,
		Service:  service,
		Notifier: notifier,
		Logger:   logger,
	})
}

func TestStartWithoutATokenAnswers(t *testing.T) {
	gateway := newGateway()
	notifier := testsupport.NewNotifier()
	agent := newAgent(t, gateway, notifier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()

	gateway.commands <- domaintelegram.Command{
		ChatID:   100,
		UserID:   200,
		Username: "tester",
		Command:  "start",
	}

	text := waitMessage(t, notifier)
	if !strings.Contains(text, "running") {
		t.Fatalf("the reply %q does not say the bot is running", text)
	}
	if !strings.Contains(text, "/start <token>") {
		t.Fatalf("the reply %q does not explain how to pair", text)
	}

	cancel()
	<-done
}

func TestStartWithAnUnknownTokenIsRefused(t *testing.T) {
	gateway := newGateway()
	notifier := testsupport.NewNotifier()
	agent := newAgent(t, gateway, notifier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()

	gateway.commands <- domaintelegram.Command{
		ChatID:   100,
		UserID:   200,
		Username: "tester",
		Command:  "start",
		Payload:  "unknown",
	}

	text := waitMessage(t, notifier)
	if !strings.Contains(text, "refused") {
		t.Fatalf("the reply %q does not refuse the pairing", text)
	}

	cancel()
	<-done
}

func TestHelpListsTheCommands(t *testing.T) {
	gateway := newGateway()
	notifier := testsupport.NewNotifier()
	agent := newAgent(t, gateway, notifier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()

	gateway.commands <- domaintelegram.Command{
		ChatID:   100,
		UserID:   200,
		Username: "tester",
		Command:  "help",
	}

	text := waitMessage(t, notifier)
	if !strings.Contains(text, "/status") {
		t.Fatalf("the reply %q does not list the commands", text)
	}

	cancel()
	<-done
}
