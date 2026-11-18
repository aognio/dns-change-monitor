package polling_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gnrfan/dns-change-monitor/pkg/app/polling"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/change"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/record"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/shared"
	"github.com/gnrfan/dns-change-monitor/pkg/testsupport"
)

func newTarget(t *testing.T, name string) record.Target {
	t.Helper()
	target, err := record.NewTarget(name, record.TypeA, "1.1.1.1:53", record.SourceManual, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	target.ID = "target-" + name
	return target
}

func newService(targets *testsupport.Targets, snapshots *testsupport.Snapshots, resolver *testsupport.Resolver, bus *testsupport.Bus) *polling.Service {
	return polling.New(
		targets,
		snapshots,
		resolver,
		testsupport.NewChanges(),
		bus,
		shared.UUIDGenerator{},
		shared.SystemClock{},
		nil,
		polling.Options{},
	)
}

func TestSweepRecordsABaselineWithoutAChange(t *testing.T) {
	target := newTarget(t, "example.com")
	targets := testsupport.NewTargets(target)
	snapshots := testsupport.NewSnapshots()
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "1.1.1.1")
	bus := testsupport.NewBus()

	result, err := newService(targets, snapshots, resolver, bus).Sweep(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Checked != 1 {
		t.Fatalf("expected one checked target, got %d", result.Checked)
	}
	if result.Changed != 0 {
		t.Fatalf("a baseline must not be a change, got %d", result.Changed)
	}
	if len(bus.Events) != 0 {
		t.Fatalf("a baseline must not publish an event, got %v", bus.Names())
	}
}

func TestSweepDetectsAChangeAndPublishesIt(t *testing.T) {
	target := newTarget(t, "example.com")
	targets := testsupport.NewTargets(target)
	snapshots := testsupport.NewSnapshots(record.Snapshot{
		ID:         "snapshot-1",
		TargetID:   target.ID,
		Values:     []string{"1.1.1.1"},
		ObservedAt: time.Now().UTC().Add(-time.Minute),
	})
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "2.2.2.2")
	bus := testsupport.NewBus()

	changes := testsupport.NewChanges()
	service := polling.New(targets, snapshots, resolver, changes, bus,
		shared.UUIDGenerator{}, shared.SystemClock{}, nil, polling.Options{})

	result, err := service.Sweep(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Changed != 1 {
		t.Fatalf("expected one change, got %d", result.Changed)
	}

	items := changes.Items()
	if len(items) != 1 {
		t.Fatalf("expected one stored change, got %d", len(items))
	}
	if len(items[0].Added) != 1 || items[0].Added[0] != "2.2.2.2" {
		t.Fatalf("unexpected additions: %v", items[0].Added)
	}
	if len(items[0].Removed) != 1 || items[0].Removed[0] != "1.1.1.1" {
		t.Fatalf("unexpected removals: %v", items[0].Removed)
	}

	names := bus.Names()
	if len(names) != 1 || names[0] != change.EventRecorded {
		t.Fatalf("unexpected published events: %v", names)
	}
}

func TestSweepKeepsGoingWhenATargetFails(t *testing.T) {
	failing := newTarget(t, "broken.com")
	working := newTarget(t, "example.com")
	targets := testsupport.NewTargets(failing, working)
	snapshots := testsupport.NewSnapshots(record.Snapshot{
		ID:         "snapshot-1",
		TargetID:   working.ID,
		Values:     []string{"1.1.1.1"},
		ObservedAt: time.Now().UTC().Add(-time.Minute),
	})
	resolver := testsupport.NewResolver()
	resolver.SetFailure(failing.Identity(), errors.New("server unreachable"))
	resolver.SetAnswer(working.Identity(), "2.2.2.2")
	bus := testsupport.NewBus()

	changes := testsupport.NewChanges()
	service := polling.New(targets, snapshots, resolver, changes, bus,
		shared.UUIDGenerator{}, shared.SystemClock{}, nil, polling.Options{})

	result, err := service.Sweep(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Failures) != 1 {
		t.Fatalf("expected one failure, got %d", len(result.Failures))
	}
	if result.Checked != 1 || result.Changed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSweepIgnoresDisabledTargets(t *testing.T) {
	target := newTarget(t, "example.com")
	target.Enabled = false
	targets := testsupport.NewTargets(target)
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "1.1.1.1")
	bus := testsupport.NewBus()

	result, err := newService(targets, testsupport.NewSnapshots(), resolver, bus).Sweep(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Checked != 0 {
		t.Fatalf("a disabled target must not be resolved, got %d", result.Checked)
	}
}

func TestSweepReportsTheBaselineWhenAsked(t *testing.T) {
	target := newTarget(t, "example.com")
	targets := testsupport.NewTargets(target)
	resolver := testsupport.NewResolver()
	resolver.SetAnswer(target.Identity(), "1.1.1.1")
	bus := testsupport.NewBus()
	changes := testsupport.NewChanges()

	service := polling.New(targets, testsupport.NewSnapshots(), resolver, changes, bus,
		shared.UUIDGenerator{}, shared.SystemClock{}, nil, polling.Options{NotifyBaseline: true})

	result, err := service.Sweep(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Changed != 1 {
		t.Fatalf("expected the baseline to be reported, got %d", result.Changed)
	}
}

func TestSweepTreatsAMissingRecordAsAnEmptyAnswer(t *testing.T) {
	target := newTarget(t, "voicebot.omniatel.omnicanal.pe")
	targets := testsupport.NewTargets(target)
	snapshots := testsupport.NewSnapshots()
	resolver := testsupport.NewResolver()
	resolver.SetFailure(target.Identity(), fmt.Errorf("%w: the name does not exist", record.ErrNoRecords))
	bus := testsupport.NewBus()
	changes := testsupport.NewChanges()

	service := polling.New(targets, snapshots, resolver, changes, bus,
		shared.UUIDGenerator{}, shared.SystemClock{}, nil, polling.Options{})

	result, err := service.Sweep(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("a name without a record is not a failure: %+v", result.Failures)
	}
	if result.Checked != 1 {
		t.Fatalf("the record must have been observed, got %d", result.Checked)
	}
}

func TestSweepReportsTheArrivalOfARecord(t *testing.T) {
	target := newTarget(t, "voicebot.omniatel.omnicanal.pe")
	targets := testsupport.NewTargets(target)
	snapshots := testsupport.NewSnapshots()
	resolver := testsupport.NewResolver()
	resolver.SetFailure(target.Identity(), fmt.Errorf("%w: the name does not exist", record.ErrNoRecords))
	bus := testsupport.NewBus()
	changes := testsupport.NewChanges()

	service := polling.New(targets, snapshots, resolver, changes, bus,
		shared.UUIDGenerator{}, shared.SystemClock{}, nil, polling.Options{})

	if _, err := service.Sweep(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The record appears with its address.
	resolver.SetAnswer(target.Identity(), "190.105.252.29")

	result, err := service.Sweep(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Changed != 1 {
		t.Fatalf("the arrival of the record must be a change, got %d", result.Changed)
	}

	items := changes.Items()
	if len(items) != 1 {
		t.Fatalf("expected one stored change, got %d", len(items))
	}
	if len(items[0].Added) != 1 || items[0].Added[0] != "190.105.252.29" {
		t.Fatalf("unexpected additions: %v", items[0].Added)
	}
}
