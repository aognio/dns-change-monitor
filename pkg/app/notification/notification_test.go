package notification_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestReportReachesThePairedSubscribersOnly(t *testing.T) {
	now := time.Now().UTC()
	paired, err := subscriber.New("subscriber-1", "alice", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	paired, err = paired.Pair(42, -1001, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pending, err := subscriber.New("subscriber-2", "bob", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subscribers := testsupport.NewSubscribers(paired, pending)
	changes := testsupport.NewChanges()
	notifier := testsupport.NewNotifier()

	service := appnotification.New(subscribers, changes, notifier, shared.SystemClock{}, nil)
	if err := service.Report(context.Background(), newChange(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if notifier.Delivered() != 1 {
		t.Fatalf("expected one delivery, got %d", notifier.Delivered())
	}
	if notifier.Targets[0].ChatID != -1001 {
		t.Fatalf("unexpected recipient: %+v", notifier.Targets[0])
	}
}

func TestReportMarksTheChangeAsNotified(t *testing.T) {
	now := time.Now().UTC()
	paired, err := subscriber.New("subscriber-1", "alice", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	paired, err = paired.Pair(42, -1001, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	changes := testsupport.NewChanges()
	detected := newChange(t)
	if err := changes.Save(context.Background(), detected); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	service := appnotification.New(testsupport.NewSubscribers(paired), changes,
		testsupport.NewNotifier(), shared.SystemClock{}, nil)
	if err := service.Report(context.Background(), detected); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pending, err := changes.ListPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("the change must be marked as notified, %d remain pending", len(pending))
	}
}

func TestReportSurvivesAFailingDelivery(t *testing.T) {
	now := time.Now().UTC()
	paired, err := subscriber.New("subscriber-1", "alice", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	paired, err = paired.Pair(42, -1001, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	notifier := testsupport.NewNotifier()
	notifier.Fail(errors.New("network unreachable"))

	service := appnotification.New(testsupport.NewSubscribers(paired), testsupport.NewChanges(),
		notifier, shared.SystemClock{}, nil)
	if err := service.Report(context.Background(), newChange(t)); err == nil {
		t.Fatal("the delivery failure must be reported")
	}
}

func TestRenderDescribesTheChange(t *testing.T) {
	body := appnotification.Render(newChange(t))
	for _, fragment := range []string{"example.com", "A", "2.2.2.2", "1.1.1.1", "Added", "Removed"} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("the message must mention %q:\n%s", fragment, body)
		}
	}
}

func newVerdictChange(t *testing.T, kind change.Kind, mode record.Mode, reason string,
	added, removed []string,
) change.Change {
	t.Helper()
	detected, err := change.NewVerdict("change-2", record.Target{
		ID:   "target-1",
		Name: "example.com",
		Type: record.TypeA,
	}, added, removed, kind, mode, reason, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return detected
}

func TestRenderKeepsTheMessageOfTheChangeModeUnchanged(t *testing.T) {
	body := appnotification.Render(newChange(t))
	if strings.Contains(body, "Assert") || strings.Contains(body, "Reason") {
		t.Fatalf("the change mode carries no verdict:\n%s", body)
	}
}

func TestRenderAddsTheVerdictToAValueChange(t *testing.T) {
	detected := newVerdictChange(t, change.KindValue, record.ModeExact,
		"expected exactly [1.2.3.4], got [5.6.7.8]", []string{"5.6.7.8"}, []string{"1.2.3.4"})

	body := appnotification.Render(detected)
	for _, fragment := range []string{
		"DNS change detected",
		"Record : example.com A",
		"Assert : exact",
		"Reason : expected exactly [1.2.3.4], got [5.6.7.8]",
		"Added:",
		"  + 5.6.7.8",
		"Removed:",
		"  - 1.2.3.4",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("the message must mention %q:\n%s", fragment, body)
		}
	}
}

func TestRenderDescribesAViolation(t *testing.T) {
	detected := newVerdictChange(t, change.KindViolated, record.ModeExact,
		"expected exactly [1.2.3.4], got [5.6.7.8]", []string{"5.6.7.8"}, []string{"1.2.3.4"})

	body := appnotification.Render(detected)
	for _, fragment := range []string{
		"DNS assertion violated",
		"Record : example.com A",
		"Assert : exact",
		"Reason : expected exactly [1.2.3.4], got [5.6.7.8]",
		"Changes since the last observation:",
		"  + 5.6.7.8",
		"  - 1.2.3.4",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("the message must mention %q:\n%s", fragment, body)
		}
	}
}

func TestRenderDescribesARecovery(t *testing.T) {
	detected := newVerdictChange(t, change.KindRecovered, record.ModeExact,
		"the answer is exactly the expected set [1.2.3.4]", []string{"1.2.3.4"}, []string{"5.6.7.8"})

	body := appnotification.Render(detected)
	for _, fragment := range []string{
		"DNS assertion recovered",
		"Assert : exact",
		"Reason : the answer is exactly the expected set [1.2.3.4]",
		"  + 1.2.3.4",
		"  - 5.6.7.8",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("the message must mention %q:\n%s", fragment, body)
		}
	}
}

func TestRenderOmitsTheChangesSectionWhenTheDifferenceIsEmpty(t *testing.T) {
	detected := newVerdictChange(t, change.KindViolated, record.ModeAny,
		"the record carries no answer", nil, nil)

	body := appnotification.Render(detected)
	if strings.Contains(body, "Changes since the last observation:") {
		t.Fatalf("the changes section must be omitted:\n%s", body)
	}
	if !strings.Contains(body, "DNS assertion violated") {
		t.Fatalf("the message must mention the violation:\n%s", body)
	}
}

func TestReportDeliversAViolationWithoutAnyDifference(t *testing.T) {
	now := time.Now().UTC()
	paired, err := subscriber.New("subscriber-1", "alice", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	paired, err = paired.Pair(42, -1001, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	notifier := testsupport.NewNotifier()
	service := appnotification.New(testsupport.NewSubscribers(paired), testsupport.NewChanges(),
		notifier, shared.SystemClock{}, nil)

	detected := newVerdictChange(t, change.KindViolated, record.ModeAny,
		"the record carries no answer", nil, nil)

	if err := service.Report(context.Background(), detected); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notifier.Delivered() != 1 {
		t.Fatalf("expected one delivery, got %d", notifier.Delivered())
	}
}
