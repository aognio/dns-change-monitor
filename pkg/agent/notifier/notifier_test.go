package notifier_test

import (
	"context"
	"testing"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/agent/notifier"
	appnotification "github.com/aognio/dns-change-monitor/pkg/app/notification"
	"github.com/aognio/dns-change-monitor/pkg/domain/change"
	"github.com/aognio/dns-change-monitor/pkg/domain/record"
	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
	"github.com/aognio/dns-change-monitor/pkg/domain/subscriber"
	"github.com/aognio/dns-change-monitor/pkg/testsupport"
)

func newChange(t *testing.T) change.Change {
	t.Helper()
	detected, err := change.New("change-1", record.Target{
		ID:   "target-1",
		Name: "example.com",
		Type: record.TypeA,
	}, []string{"2.2.2.2"}, []string{"1.1.1.1"}, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return detected
}

func TestAgentReportsTheChangesWaitingForAReport(t *testing.T) {
	changes := testsupport.NewChanges()
	detected := newChange(t)
	if err := changes.Save(context.Background(), detected); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deliverer := testsupport.NewNotifier()
	service := appnotification.New(pairedSubscribers(t), changes, deliverer, shared.SystemClock{}, nil)

	agent := notifier.New(notifier2Options(service))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = agent.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if deliverer.Delivered() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the waiting change must be reported")
}

func TestAgentReportsTheChangesCarriedByTheBus(t *testing.T) {
	changes := testsupport.NewChanges()
	deliverer := testsupport.NewNotifier()
	service := appnotification.New(pairedSubscribers(t), changes, deliverer, shared.SystemClock{}, nil)

	bus := testsupport.NewBus()
	agent := notifier.New(notifier2Options(service, bus))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = agent.Run(ctx) }()

	// The agent subscribes to the bus when it starts.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		detected := newChange(t)
		if err := changes.Save(ctx, detected); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := bus.Publish(ctx, change.RecordedEvent{Change: detected, Timestamp: time.Now().UTC()}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deliverer.Delivered() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the change must be reported")
}

// notifier2Options builds the options of the notification agent.
func notifier2Options(service *appnotification.Service, buses ...shared.EventBus) notifier.Options {
	options := notifier.Options{
		Service:         service,
		PendingInterval: 50 * time.Millisecond,
	}
	if len(buses) > 0 {
		options.Bus = buses[0]
	}
	return options
}

// pairedSubscribers builds a repository holding one paired account.
func pairedSubscribers(t *testing.T) *testsupport.Subscribers {
	t.Helper()
	now := time.Now().UTC()
	entry, err := subscriber.New("subscriber-1", "alice", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, err = entry.Pair(42, -1001, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return testsupport.NewSubscribers(entry)
}
