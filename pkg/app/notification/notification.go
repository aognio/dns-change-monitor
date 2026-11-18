// Package notification implements the use case that reports detected changes
// to the paired Telegram accounts.
package notification

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/domain/change"
	"github.com/aognio/dns-change-monitor/pkg/domain/notification"
	"github.com/aognio/dns-change-monitor/pkg/domain/record"
	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
	"github.com/aognio/dns-change-monitor/pkg/domain/subscriber"
)

// Service delivers change reports to subscribers.
type Service struct {
	subscribers subscriber.Repository
	changes     change.Repository
	notifier    notification.Notifier
	clock       shared.Clock
	logger      *slog.Logger
}

// New builds the notification service.
func New(
	subscribers subscriber.Repository,
	changes change.Repository,
	notifier notification.Notifier,
	clock shared.Clock,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		subscribers: subscribers,
		changes:     changes,
		notifier:    notifier,
		clock:       clock,
		logger:      logger,
	}
}

// Report delivers a change to every paired subscriber and records that the
// change has been announced. It reports the first delivery failure, if any.
func (s *Service) Report(ctx context.Context, detected change.Change) error {
	if detected.IsEmpty() && detected.Kind == change.KindValue {
		return nil
	}

	recipients, err := s.pairedRecipients(ctx)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		s.logger.Warn("no paired subscriber to notify", slog.String("target", detected.TargetName))
		return nil
	}

	body := Render(detected)
	var firstErr error
	delivered := 0
	for _, recipient := range recipients {
		if err := s.notifier.Send(ctx, recipient, notification.Message{Text: body}); err != nil {
			s.logger.Error("notification delivery failed",
				slog.String("recipient", recipient.Username),
				slog.String("error", err.Error()))
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delivered++
	}

	if delivered > 0 {
		if err := s.changes.MarkNotified(ctx, detected.ID, s.clock.Now()); err != nil {
			return fmt.Errorf("mark change notified: %w", err)
		}
		s.logger.Info("change reported",
			slog.String("target", detected.TargetName),
			slog.Int("recipients", delivered))
	}
	return firstErr
}

// ReportPending delivers the changes that were detected but never announced,
// for instance because the daemon was stopped before it could.
func (s *Service) ReportPending(ctx context.Context) error {
	pending, err := s.changes.ListPending(ctx)
	if err != nil {
		return fmt.Errorf("list pending changes: %w", err)
	}
	for _, detected := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.Report(ctx, detected); err != nil {
			s.logger.Error("pending change not reported",
				slog.String("target", detected.TargetName),
				slog.String("error", err.Error()))
		}
	}
	return nil
}

func (s *Service) pairedRecipients(ctx context.Context) ([]notification.Recipient, error) {
	all, err := s.subscribers.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subscribers: %w", err)
	}
	recipients := make([]notification.Recipient, 0, len(all))
	for _, candidate := range all {
		if !candidate.IsPaired() {
			continue
		}
		recipients = append(recipients, notification.Recipient{
			ChatID:   candidate.ChatID,
			Username: candidate.Username,
		})
	}
	return recipients, nil
}

// Render builds the message body describing a change.
func Render(detected change.Change) string {
	switch detected.Kind {
	case change.KindViolated:
		return renderAssertion(detected, "DNS assertion violated")
	case change.KindRecovered:
		return renderAssertion(detected, "DNS assertion recovered")
	}
	return renderValue(detected)
}

// renderValue builds the message body of a value change. A record declaring
// an expectation carries the verdict of the expectation next to the change.
func renderValue(detected change.Change) string {
	var builder strings.Builder
	builder.WriteString("DNS change detected\n")
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("Record : %s %s", detected.TargetName, detected.RecordType))
	builder.WriteString("\n")
	if declared(detected) {
		builder.WriteString(fmt.Sprintf("Assert : %s", detected.Mode))
		builder.WriteString("\n")
	}
	builder.WriteString(fmt.Sprintf("Time   : %s", detected.DetectedAt.UTC().Format(time.RFC3339)))
	builder.WriteString("\n")
	if declared(detected) {
		builder.WriteString(fmt.Sprintf("Reason : %s", detected.Reason))
		builder.WriteString("\n")
	}
	if len(detected.Added) > 0 {
		builder.WriteString("\nAdded:\n")
		for _, value := range detected.Added {
			builder.WriteString(fmt.Sprintf("  + %s", value))
			builder.WriteString("\n")
		}
	}
	if len(detected.Removed) > 0 {
		builder.WriteString("\nRemoved:\n")
		for _, value := range detected.Removed {
			builder.WriteString(fmt.Sprintf("  - %s", value))
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

// renderAssertion builds the message body of a violated or recovered
// expectation.
func renderAssertion(detected change.Change, header string) string {
	var builder strings.Builder
	builder.WriteString(header)
	builder.WriteString("\n")
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("Record : %s %s", detected.TargetName, detected.RecordType))
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("Assert : %s", detected.Mode))
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("Time   : %s", detected.DetectedAt.UTC().Format(time.RFC3339)))
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("Reason : %s", detected.Reason))
	builder.WriteString("\n")
	if len(detected.Added) == 0 && len(detected.Removed) == 0 {
		return builder.String()
	}
	builder.WriteString("\nChanges since the last observation:\n")
	for _, value := range detected.Added {
		builder.WriteString(fmt.Sprintf("  + %s", value))
		builder.WriteString("\n")
	}
	for _, value := range detected.Removed {
		builder.WriteString(fmt.Sprintf("  - %s", value))
		builder.WriteString("\n")
	}
	return builder.String()
}

// declared reports whether the change carries the verdict of an expectation.
func declared(detected change.Change) bool {
	return detected.Mode != "" && detected.Mode != record.ModeChange
}
