// Package poller holds the agent that keeps the monitored DNS names fresh.
package poller

import (
	"context"
	"log/slog"
	"time"

	"github.com/gnrfan/dns-change-monitor/pkg/app/polling"
)

// Agent resolves the monitored DNS names on a fixed schedule and records the
// differences it finds.
type Agent struct {
	service  *polling.Service
	interval time.Duration
	logger   *slog.Logger
}

// Options configure the polling agent.
type Options struct {
	// Service performs the sweep.
	Service *polling.Service
	// Interval is the delay between two consecutive sweeps.
	Interval time.Duration
	// Logger receives the diagnostics of the agent.
	Logger *slog.Logger
}

// New builds the polling agent.
func New(options Options) *Agent {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	interval := options.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	return &Agent{
		service:  options.Service,
		interval: interval,
		logger:   logger,
	}
}

// Name implements agent.Agent.
func (a *Agent) Name() string { return "dns-poller" }

// Goal implements agent.Agent.
func (a *Agent) Goal() string {
	return "resolve the monitored DNS names regularly and detect their changes"
}

// Run implements agent.Agent.
func (a *Agent) Run(ctx context.Context) error {
	a.sweep(ctx)

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			a.sweep(ctx)
		}
	}
}

// sweep performs one resolution of every monitored target.
func (a *Agent) sweep(ctx context.Context) {
	result, err := a.service.Sweep(ctx)
	if err != nil {
		a.logger.Error("sweep failed", slog.String("error", err.Error()))
		return
	}
	if failure := result.Err(); failure != nil {
		a.logger.Warn("sweep completed with failures",
			slog.Int("checked", result.Checked),
			slog.Int("changed", result.Changed),
			slog.String("error", failure.Error()))
		return
	}
	a.logger.Debug("sweep completed",
		slog.Int("checked", result.Checked),
		slog.Int("changed", result.Changed))
}
