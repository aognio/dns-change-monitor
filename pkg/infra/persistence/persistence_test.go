package persistence_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gnrfan/dns-change-monitor/pkg/domain/change"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/pairing"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/record"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/shared"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/subscriber"
	"github.com/gnrfan/dns-change-monitor/pkg/infra/persistence"
)

func newRepositories(t *testing.T) persistence.Repositories {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dnsmon.db")
	db, err := persistence.Open(path, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return persistence.NewRepositories(db)
}

func TestTargetRoundTrip(t *testing.T) {
	repositories := newRepositories(t)
	ctx := context.Background()
	now := time.Now().UTC()

	target, err := record.NewTarget("Example.com", record.TypeA, "1.1.1.1:53", record.SourceConfig, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	target.ID = "target-1"

	if err := repositories.Targets.Upsert(ctx, target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := repositories.Targets.Get(ctx, "target-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.Name != "example.com" {
		t.Fatalf("unexpected name: %q", stored.Name)
	}
	if stored.Type != record.TypeA {
		t.Fatalf("unexpected type: %q", stored.Type)
	}
}

func TestUpsertKeepsTheIdentifierOfAnExistingTarget(t *testing.T) {
	repositories := newRepositories(t)
	ctx := context.Background()
	now := time.Now().UTC()

	target, err := record.NewTarget("example.com", record.TypeA, "1.1.1.1:53", record.SourceManual, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	target.ID = "target-1"
	if err := repositories.Targets.Upsert(ctx, target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := target
	updated.ID = "different"
	updated.Enabled = false
	if err := repositories.Targets.Upsert(ctx, updated); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := repositories.Targets.Get(ctx, "target-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.Enabled {
		t.Fatal("the target must be disabled")
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	repositories := newRepositories(t)
	ctx := context.Background()

	snapshot := record.Snapshot{
		ID:         "snapshot-1",
		TargetID:   "target-1",
		Values:     []string{"1.1.1.1", "2.2.2.2"},
		TTL:        90 * time.Second,
		ObservedAt: time.Now().UTC(),
	}
	if err := repositories.Snapshots.Save(ctx, snapshot); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := repositories.Snapshots.Latest(ctx, "target-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stored.Values) != 2 || stored.Values[0] != "1.1.1.1" {
		t.Fatalf("unexpected values: %v", stored.Values)
	}
	if stored.TTL != 90*time.Second {
		t.Fatalf("unexpected time to live: %s", stored.TTL)
	}
}

func TestMissingSnapshotReportsNotFound(t *testing.T) {
	repositories := newRepositories(t)
	if _, err := repositories.Snapshots.Latest(context.Background(), "unknown"); err == nil {
		t.Fatal("expected an error for a missing snapshot")
	}
}

func TestChangeRoundTripAndNotification(t *testing.T) {
	repositories := newRepositories(t)
	ctx := context.Background()
	now := time.Now().UTC()

	detected, err := change.New("change-1", record.Target{
		ID:   "target-1",
		Name: "example.com",
		Type: record.TypeA,
	}, []string{"2.2.2.2"}, []string{"1.1.1.1"}, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repositories.Changes.Save(ctx, detected); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pending, err := repositories.Changes.ListPending(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected one pending change, got %d", len(pending))
	}

	if err := repositories.Changes.MarkNotified(ctx, "change-1", now); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pending, err = repositories.Changes.ListPending(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected no pending change, got %d", len(pending))
	}
}

func TestSubscriberRoundTrip(t *testing.T) {
	repositories := newRepositories(t)
	ctx := context.Background()
	now := time.Now().UTC()

	entry, err := subscriber.New("subscriber-1", "alice", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repositories.Subscribers.Upsert(ctx, entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	paired, err := entry.Pair(42, -1001, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repositories.Subscribers.Upsert(ctx, paired); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := repositories.Subscribers.FindByTelegramUserID(ctx, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stored.IsPaired() {
		t.Fatal("the subscriber must be paired")
	}

	byName, err := repositories.Subscribers.FindByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if byName.ID != "subscriber-1" {
		t.Fatalf("unexpected identifier: %q", byName.ID)
	}
}

func TestTokenRoundTrip(t *testing.T) {
	repositories := newRepositories(t)
	ctx := context.Background()
	now := time.Now().UTC()

	token, err := pairing.NewToken("token-1", time.Minute, "cli", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repositories.Tokens.Create(ctx, token); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := repositories.Tokens.Get(ctx, "token-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.IsConsumed() {
		t.Fatal("a fresh token must not be consumed")
	}

	consumed, err := stored.Consume(now, "subscriber-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repositories.Tokens.Save(ctx, consumed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reloaded, err := repositories.Tokens.Get(ctx, "token-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reloaded.IsConsumed() {
		t.Fatal("the token must be consumed")
	}
}

func TestUnknownTokenReportsNotFound(t *testing.T) {
	repositories := newRepositories(t)
	_, err := repositories.Tokens.Get(context.Background(), "unknown")
	if err == nil {
		t.Fatal("expected an error for a missing token")
	}
	if !isNotFound(err) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func isNotFound(err error) bool {
	return err != nil && (err == shared.ErrNotFound ||
		len(err.Error()) > 0 && containsText(err.Error(), shared.ErrNotFound.Error()))
}

func containsText(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
