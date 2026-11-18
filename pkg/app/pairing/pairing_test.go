package pairing_test

import (
	"context"
	"testing"
	"time"

	apppairing "github.com/aognio/dns-change-monitor/pkg/app/pairing"
	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
	"github.com/aognio/dns-change-monitor/pkg/domain/subscriber"
	"github.com/aognio/dns-change-monitor/pkg/domain/telegram"
	"github.com/aognio/dns-change-monitor/pkg/testsupport"
)

func newService(tokens *testsupport.Tokens, subscribers *testsupport.Subscribers, allowlist ...string) *apppairing.Service {
	return apppairing.New(tokens, subscribers, allowlist,
		shared.UUIDGenerator{}, shared.SystemClock{}, nil)
}

func TestPairingAuthorizesAnAllowedAccount(t *testing.T) {
	tokens := testsupport.NewTokens()
	subscribers := testsupport.NewSubscribers()
	service := newService(tokens, subscribers, "alice")

	token, err := service.Issue(context.Background(), time.Minute, "cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome, err := service.Pair(context.Background(), telegram.Command{
		ChatID:   -1001,
		UserID:   42,
		Username: "alice",
		Payload:  token.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !outcome.Paired {
		t.Fatalf("the pairing was refused: %s", outcome.Reason)
	}

	stored, err := subscribers.FindByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stored.IsPaired() {
		t.Fatal("the subscriber must be paired")
	}
	if stored.ChatID != -1001 || stored.TelegramUserID != 42 {
		t.Fatalf("unexpected identity: %+v", stored)
	}
}

func TestPairingRefusesAnAccountOutsideTheAllowlist(t *testing.T) {
	tokens := testsupport.NewTokens()
	subscribers := testsupport.NewSubscribers()
	service := newService(tokens, subscribers, "alice")

	token, err := service.Issue(context.Background(), time.Minute, "cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome, err := service.Pair(context.Background(), telegram.Command{
		ChatID:   -1002,
		UserID:   43,
		Username: "mallory",
		Payload:  token.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Paired {
		t.Fatal("the pairing must be refused")
	}
	if outcome.Reason == "" {
		t.Fatal("the refusal must carry a reason")
	}

	stored, err := tokens.Get(context.Background(), token.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.IsConsumed() {
		t.Fatal("a refused pairing must not consume the token")
	}
}

func TestPairingRefusesAUsedToken(t *testing.T) {
	tokens := testsupport.NewTokens()
	subscribers := testsupport.NewSubscribers()
	service := newService(tokens, subscribers, "alice")

	token, err := service.Issue(context.Background(), time.Minute, "cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	command := telegram.Command{ChatID: -1001, UserID: 42, Username: "alice", Payload: token.ID}
	if _, err := service.Pair(context.Background(), command); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome, err := service.Pair(context.Background(), command)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Paired {
		t.Fatal("a token must only work once")
	}
}

func TestPairingRefusesAnUnknownToken(t *testing.T) {
	service := newService(testsupport.NewTokens(), testsupport.NewSubscribers(), "alice")

	outcome, err := service.Pair(context.Background(), telegram.Command{
		ChatID:   -1001,
		UserID:   42,
		Username: "alice",
		Payload:  "not-a-token",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Paired {
		t.Fatal("an unknown token must be refused")
	}
}

func TestPairingWithoutATokenIsRefused(t *testing.T) {
	service := newService(testsupport.NewTokens(), testsupport.NewSubscribers())

	outcome, err := service.Pair(context.Background(), telegram.Command{
		ChatID:   -1001,
		UserID:   42,
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Paired || outcome.Reason == "" {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
}

func TestPairingUpdatesAnExistingSubscriber(t *testing.T) {
	now := time.Now().UTC()
	existing, err := subscriber.New("subscriber-1", "alice", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	subscribers := testsupport.NewSubscribers(existing)
	tokens := testsupport.NewTokens()
	service := newService(tokens, subscribers, "alice")

	token, err := service.Issue(context.Background(), time.Minute, "cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outcome, err := service.Pair(context.Background(), telegram.Command{
		ChatID:   -1001,
		UserID:   42,
		Username: "alice",
		Payload:  token.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !outcome.Paired {
		t.Fatalf("the pairing was refused: %s", outcome.Reason)
	}

	stored, err := subscribers.Get(context.Background(), "subscriber-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stored.IsPaired() {
		t.Fatal("the existing subscriber must be paired")
	}
}
