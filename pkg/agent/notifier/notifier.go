// Package notifier holds the agent that reports detected changes to the
// paired Telegram accounts.
package notifier

import (
	"context"
	"log/slog"
	"time"

	"github.com/gnrfan/dns-change-monitor/pkg/app/notification"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/change"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/shared"
)

// Agent turns change events into Telegram notifications.
type Agent struct {
	service         *notification.Service
	bus             shared.EventBus
	pendingInterval time.Duration
	logger          *slog.Logger
}

// Options configure the notification agent.
type Options struct {
	// Service delivers the reports.
	Service *notification.Service
	// Bus carries the change events.
	Bus shared.EventBus
	// PendingInterval is how often the changes that are still waiting for
	// a report are delivered again.
	PendingInterval time.Duration
	// Logger receives the diagnostics of the agent.
	Logger *slog.Logger
}

// New builds the notification agent.
func New(options Options) *Agent {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	interval := options.PendingInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Agent{
		service:         options.Service,
		bus:             options.Bus,
		pendingInterval: interval,
		logger:          logger,
	}
}

// Name implements agent.Agent.
func (a *Agent) Name() string { return "change-notifier" }

// Goal implements agent.Agent.
func (a *Agent) Goal() string {
	return "deliver every detected DNS change to the paired Telegram accounts"
}

// Run implements agent.Agent.
func (a *Agent) Run(ctx context.Context) error {
	if a.bus != nil {
		a.bus.Subscribe(change.EventRecorded, a.onChange)
	}

	// The changes detected while the daemon was down are still waiting for a
	// report.
	if err := a.service.ReportPending(ctx); err != nil {
		a.logger.Error("pending changes could not be reported", slog.String("error", err.Error()))
	}

	// A change whose delivery failed, or that was detected before this
	// agent subscribed to the events, is picked up again here.
	ticker := time.NewTicker(a.pendingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := a.service.ReportPending(ctx); err != nil {
				a.logger.Error("pending changes could not be reported", slog.String("error", err.Error()))
			}
		}
	}
}

// onChange reports a freshly detected change.
func (a *Agent) onChange(ctx context.Context, event shared.Event) error {
	recorded, ok := event.(change.RecordedEvent)
	if !ok {
		return nil
	}
	if err := a.service.Report(ctx, recorded.Change); err != nil {
		a.logger.Error("change not reported",
			slog.String("target", recorded.Change.TargetName),
			slog.String("error", err.Error()))
	}
	return nil
}
