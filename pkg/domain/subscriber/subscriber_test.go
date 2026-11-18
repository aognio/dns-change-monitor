package subscriber

import (
	"testing"
	"time"
)

func TestPairBindsTheTelegramIdentity(t *testing.T) {
	now := time.Now().UTC()
	entry, err := New("id", "@Alice", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Username != "alice" {
		t.Fatalf("unexpected username: %q", entry.Username)
	}
	if entry.IsPaired() {
		t.Fatal("a pending subscriber must not be paired")
	}

	paired, err := entry.Pair(42, -1001, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !paired.IsPaired() {
		t.Fatal("a paired subscriber must report as paired")
	}
	if paired.ChatID != -1001 || paired.TelegramUserID != 42 {
		t.Fatalf("unexpected identity: %+v", paired)
	}
}

func TestPairRejectsAnIncompleteIdentity(t *testing.T) {
	now := time.Now().UTC()
	entry, err := New("id", "alice", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := entry.Pair(0, 0, now); err == nil {
		t.Fatal("expected an error for an incomplete identity")
	}
}

func TestCanonicalUsernameRejectsEmptyValues(t *testing.T) {
	if _, err := CanonicalUsername("  "); err == nil {
		t.Fatal("expected an error for an empty username")
	}
}
