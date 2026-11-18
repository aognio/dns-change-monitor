package polling_test

import (
	"context"
	"testing"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/app/polling"
	"github.com/aognio/dns-change-monitor/pkg/domain/change"
	"github.com/aognio/dns-change-monitor/pkg/domain/record"
	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
	"github.com/aognio/dns-change-monitor/pkg/testsupport"
)

func newCheckedTarget(t *testing.T, name string, mode record.Mode, expect ...string) record.Target {
	t.Helper()
	target := newTarget(t, name)
	check, err := record.NewCheck(mode, expect)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	target.Check = check
	return target
}

func newSweep(t *testing.T, target record.Target, snapshots *testsupport.Snapshots,
	resolver *testsupport.Resolver, changes *testsupport.Changes, bus *testsupport.Bus,
	options polling.Options,
) polling.Result {
	t.Helper()
	service := polling.New(testsupport.NewTargets(target), snapshots, resolver, changes, bus,
		shared.UUIDGenerator{}, shared.SystemClock{}, nil, options)

	result, err := service.Sweep(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return result
}

func TestSweepPublishesAViolationOnTheFirstObservation(t *testing.T) {
	target := newCheckedTarget(t, "example.com", record.ModeExact, "1.2.3.4")
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "5.6.7.8")
	changes := testsupport.NewChanges()
	bus := testsupport.NewBus()

	result := newSweep(t, target, testsupport.NewSnapshots(), resolver, changes, bus, polling.Options{})

	if result.Violations != 1 {
		t.Fatalf("expected one violation, got %d", result.Violations)
	}
	if result.Changed != 1 {
		t.Fatalf("expected the violation to be reported, got %d", result.Changed)
	}

	items := changes.Items()
	if len(items) != 1 {
		t.Fatalf("expected one stored change, got %d", len(items))
	}
	if items[0].Kind != change.KindViolated {
		t.Fatalf("expected a violated change, got %q", items[0].Kind)
	}
	if items[0].Mode != record.ModeExact {
		t.Fatalf("expected the exact mode, got %q", items[0].Mode)
	}
	if items[0].Reason == "" {
		t.Fatal("the violation must carry a reason")
	}
	if len(bus.Events) != 1 {
		t.Fatalf("expected one published event, got %d", len(bus.Events))
	}
}

func TestSweepPublishesAViolationAfterASatisfiedObservation(t *testing.T) {
	target := newCheckedTarget(t, "example.com", record.ModeExact, "1.2.3.4")
	satisfied := true
	snapshots := testsupport.NewSnapshots(record.Snapshot{
		ID:         "snapshot-1",
		TargetID:   target.ID,
		Values:     []string{"1.2.3.4"},
		Satisfied:  &satisfied,
		ObservedAt: time.Now().UTC().Add(-time.Minute),
	})
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "5.6.7.8")
	changes := testsupport.NewChanges()

	result := newSweep(t, target, snapshots, resolver, changes, testsupport.NewBus(), polling.Options{})

	if result.Violations != 1 {
		t.Fatalf("expected one violation, got %d", result.Violations)
	}
	items := changes.Items()
	if len(items) != 1 || items[0].Kind != change.KindViolated {
		t.Fatalf("expected one violated change, got %+v", items)
	}
}

func TestSweepPublishesARecovery(t *testing.T) {
	target := newCheckedTarget(t, "example.com", record.ModeExact, "1.2.3.4")
	violated := false
	snapshots := testsupport.NewSnapshots(record.Snapshot{
		ID:         "snapshot-1",
		TargetID:   target.ID,
		Values:     []string{"5.6.7.8"},
		Satisfied:  &violated,
		ObservedAt: time.Now().UTC().Add(-time.Minute),
	})
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "1.2.3.4")
	changes := testsupport.NewChanges()

	result := newSweep(t, target, snapshots, resolver, changes, testsupport.NewBus(), polling.Options{})

	if result.Violations != 0 {
		t.Fatalf("a recovered expectation is not a violation, got %d", result.Violations)
	}
	items := changes.Items()
	if len(items) != 1 || items[0].Kind != change.KindRecovered {
		t.Fatalf("expected one recovered change, got %+v", items)
	}
}

func TestSweepDoesNotPublishASteadyViolationAgain(t *testing.T) {
	target := newCheckedTarget(t, "example.com", record.ModeExact, "1.2.3.4")
	violated := false
	snapshots := testsupport.NewSnapshots(record.Snapshot{
		ID:         "snapshot-1",
		TargetID:   target.ID,
		Values:     []string{"5.6.7.8"},
		Satisfied:  &violated,
		ObservedAt: time.Now().UTC().Add(-time.Minute),
	})
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "5.6.7.8")
	changes := testsupport.NewChanges()
	bus := testsupport.NewBus()

	result := newSweep(t, target, snapshots, resolver, changes, bus, polling.Options{})

	if result.Violations != 1 {
		t.Fatalf("the expectation is still violated, got %d", result.Violations)
	}
	if result.Changed != 0 {
		t.Fatalf("a steady state must not be a change, got %d", result.Changed)
	}
	if len(changes.Items()) != 0 {
		t.Fatalf("a steady violation must not be published again, got %+v", changes.Items())
	}
	if len(bus.Events) != 0 {
		t.Fatalf("a steady violation must not publish an event, got %d", len(bus.Events))
	}
}

func TestSweepPublishesAValueChangeThatKeepsTheExpectationSatisfied(t *testing.T) {
	target := newCheckedTarget(t, "example.com", record.ModeAny)
	satisfied := true
	snapshots := testsupport.NewSnapshots(record.Snapshot{
		ID:         "snapshot-1",
		TargetID:   target.ID,
		Values:     []string{"1.1.1.1"},
		Satisfied:  &satisfied,
		ObservedAt: time.Now().UTC().Add(-time.Minute),
	})
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "2.2.2.2")
	changes := testsupport.NewChanges()

	result := newSweep(t, target, snapshots, resolver, changes, testsupport.NewBus(), polling.Options{})

	if result.Changed != 1 {
		t.Fatalf("a difference is always reported, got %d", result.Changed)
	}
	items := changes.Items()
	if len(items) != 1 {
		t.Fatalf("expected one stored change, got %d", len(items))
	}
	if items[0].Kind != change.KindValue {
		t.Fatalf("expected a value change, got %q", items[0].Kind)
	}
	if items[0].Mode != record.ModeAny {
		t.Fatalf("expected the any mode, got %q", items[0].Mode)
	}
	if items[0].Reason == "" {
		t.Fatal("the change must carry the verdict of the expectation")
	}
	if len(items[0].Added) != 1 || items[0].Added[0] != "2.2.2.2" {
		t.Fatalf("unexpected additions: %v", items[0].Added)
	}

	latest, err := snapshots.Latest(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(latest.Values) != 1 || latest.Values[0] != "2.2.2.2" {
		t.Fatalf("the snapshot must carry the new values, got %v", latest.Values)
	}
}

func TestSweepReportsTheDifferenceAgainstThePreviousObservation(t *testing.T) {
	target := newCheckedTarget(t, "example.com", record.ModeExact, "1.2.3.4")
	satisfied := true
	snapshots := testsupport.NewSnapshots(record.Snapshot{
		ID:         "snapshot-1",
		TargetID:   target.ID,
		Values:     []string{"1.2.3.4"},
		Satisfied:  &satisfied,
		ObservedAt: time.Now().UTC().Add(-time.Minute),
	})
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "5.6.7.8")
	changes := testsupport.NewChanges()

	newSweep(t, target, snapshots, resolver, changes, testsupport.NewBus(), polling.Options{})

	items := changes.Items()
	if len(items) != 1 {
		t.Fatalf("expected one stored change, got %d", len(items))
	}
	if len(items[0].Added) != 1 || items[0].Added[0] != "5.6.7.8" {
		t.Fatalf("unexpected additions: %v", items[0].Added)
	}
	if len(items[0].Removed) != 1 || items[0].Removed[0] != "1.2.3.4" {
		t.Fatalf("unexpected removals: %v", items[0].Removed)
	}
}

func TestSweepStoresTheVerdictWithTheSnapshot(t *testing.T) {
	target := newCheckedTarget(t, "example.com", record.ModeRegex, "^v=spf1")
	snapshots := testsupport.NewSnapshots()
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "v=spf1 -all")

	newSweep(t, target, snapshots, resolver, testsupport.NewChanges(), testsupport.NewBus(), polling.Options{})

	latest, err := snapshots.Latest(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest.Satisfied == nil {
		t.Fatal("a declared mode stores a verdict with the snapshot")
	}
	if !*latest.Satisfied {
		t.Fatalf("the answer matches the pattern, got the reason %q", latest.Reason)
	}
	if latest.Reason == "" {
		t.Fatal("the snapshot must carry the reason of the verdict")
	}
}

func TestSweepCountsEveryViolationOfTheSweep(t *testing.T) {
	violated := newCheckedTarget(t, "violated.com", record.ModeAny)
	satisfied := newCheckedTarget(t, "satisfied.com", record.ModeAny)

	resolver := testsupport.NewResolver()
	resolver.SetFailure(violated.Identity(), record.ErrNoRecords)
	resolver.SetAnswer(satisfied.Identity(), "2.2.2.2")

	targets := testsupport.NewTargets(violated, satisfied)
	snapshots := testsupport.NewSnapshots()
	changes := testsupport.NewChanges()

	service := polling.New(targets, snapshots, resolver, changes, testsupport.NewBus(),
		shared.UUIDGenerator{}, shared.SystemClock{}, nil, polling.Options{})

	result, err := service.Sweep(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Violations != 1 {
		t.Fatalf("expected one violation, got %d", result.Violations)
	}
	if result.Checked != 2 {
		t.Fatalf("expected two checked targets, got %d", result.Checked)
	}
}
