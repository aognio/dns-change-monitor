package pairing

import (
	"testing"
	"time"
)

func TestTokenLifecycle(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	token, err := NewToken("token-id", 30*time.Minute, "cli", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !token.IsUsable(now) {
		t.Fatal("a fresh token must be usable")
	}

	consumed, err := token.Consume(now.Add(time.Minute), "subscriber-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !consumed.IsConsumed() {
		t.Fatal("the token must be marked as consumed")
	}
	if consumed.ConsumedBy != "subscriber-id" {
		t.Fatalf("unexpected consumer: %q", consumed.ConsumedBy)
	}
	if _, err := consumed.Consume(now.Add(2*time.Minute), "other"); err == nil {
		t.Fatal("expected an error when a consumed token is redeemed twice")
	}
}

func TestExpiredTokenCannotBeConsumed(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	token, err := NewToken("token-id", time.Minute, "cli", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token.IsUsable(now.Add(2 * time.Minute)) {
		t.Fatal("an expired token must not be usable")
	}
	if _, err := token.Consume(now.Add(2*time.Minute), "subscriber-id"); err == nil {
		t.Fatal("expected an error when an expired token is redeemed")
	}
}

func TestNewTokenRejectsAnEmptyIdentifier(t *testing.T) {
	if _, err := NewToken("", time.Minute, "cli", time.Now()); err == nil {
		t.Fatal("expected an error for an empty identifier")
	}
}

func TestConsumeRequiresASubscriber(t *testing.T) {
	now := time.Now().UTC()
	token, err := NewToken("token-id", time.Minute, "cli", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := token.Consume(now, ""); err == nil {
		t.Fatal("expected an error when no subscriber is given")
	}
}
