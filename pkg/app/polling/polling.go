// Package polling implements the use case that resolves monitored DNS names
// and detects the differences between consecutive resolutions.
package polling

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/aognio/dns-change-monitor/pkg/domain/change"
	"github.com/aognio/dns-change-monitor/pkg/domain/record"
	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
)

// Failure describes a target that could not be resolved during a sweep.
type Failure struct {
	Target record.Target
	Err    error
}

// Result summarises one sweep across every enabled target.
type Result struct {
	// Checked is the number of targets that were resolved.
	Checked int
	// Changed is the number of differences that were detected.
	Changed int
	// Failures lists the targets that could not be resolved.
	Failures []Failure
}

// Err returns a single error describing every failure of the sweep.
func (r Result) Err() error {
	if len(r.Failures) == 0 {
		return nil
	}
	messages := make([]string, 0, len(r.Failures))
	for _, failure := range r.Failures {
		messages = append(messages, fmt.Sprintf("%s: %v", failure.Target.Name, failure.Err))
	}
	return fmt.Errorf("resolve %d target(s): %s", len(r.Failures), join(messages, "; "))
}

func join(items []string, sep string) string {
	result := ""
	for index, item := range items {
		if index > 0 {
			result += sep
		}
		result += item
	}
	return result
}

// Service performs the sweep of monitored DNS names.
type Service struct {
	targets   record.TargetRepository
	snapshots record.SnapshotRepository
	resolver  record.Resolver
	changes   change.Repository
	bus       shared.EventBus
	ids       shared.IDGenerator
	clock     shared.Clock
	logger    *slog.Logger
	// notifyBaseline reports the first observation of a target as a change.
	notifyBaseline bool
}

// Options configure the polling service.
type Options struct {
	// NotifyBaseline reports the first observation of a target.
	NotifyBaseline bool
}

// New builds the polling service.
func New(
	targets record.TargetRepository,
	snapshots record.SnapshotRepository,
	resolver record.Resolver,
	changes change.Repository,
	bus shared.EventBus,
	ids shared.IDGenerator,
	clock shared.Clock,
	logger *slog.Logger,
	options Options,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		targets:        targets,
		snapshots:      snapshots,
		resolver:       resolver,
		changes:        changes,
		bus:            bus,
		ids:            ids,
		clock:          clock,
		logger:         logger,
		notifyBaseline: options.NotifyBaseline,
	}
}

// Sweep resolves every enabled target once and records the differences it
// finds. A target that fails to resolve does not stop the sweep.
func (s *Service) Sweep(ctx context.Context) (Result, error) {
	targets, err := s.targets.List(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list targets: %w", err)
	}

	result := Result{}
	for _, target := range targets {
		if !target.Enabled {
			continue
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		observed, err := s.observe(ctx, target)
		if err != nil {
			result.Failures = append(result.Failures, Failure{Target: target, Err: err})
			s.logger.Warn("target resolution failed",
				slog.String("target", target.Name),
				slog.String("type", string(target.Type)),
				slog.String("error", err.Error()))
			continue
		}

		result.Checked++
		if observed != nil {
			result.Changed++
		}
	}
	return result, nil
}

// observe resolves a target and persists the outcome. It returns the detected
// change, or nil when nothing changed.
func (s *Service) observe(ctx context.Context, target record.Target) (*change.Change, error) {
	resolution, err := s.resolver.Resolve(ctx, target)
	if err != nil {
		// A name without a record is an observation, not a failure: it is
		// compared with the previous one like any other answer.
		if !errors.Is(err, record.ErrNoRecords) {
			return nil, err
		}
		resolution = record.Resolution{}
	}

	values := record.NormaliseValues(resolution.Values)
	now := s.clock.Now()

	previous, err := s.snapshots.Latest(ctx, target.ID)
	switch {
	case err == nil:
	case errors.Is(err, shared.ErrNotFound):
		if err := s.snapshots.Save(ctx, record.Snapshot{
			ID:         s.ids.NewID(),
			TargetID:   target.ID,
			Values:     values,
			TTL:        resolution.TTL,
			ObservedAt: now,
		}); err != nil {
			return nil, fmt.Errorf("save baseline snapshot: %w", err)
		}
		if !s.notifyBaseline {
			s.logger.Info("baseline recorded",
				slog.String("target", target.Name),
				slog.String("type", string(target.Type)),
				slog.Int("values", len(values)))
			return nil, nil
		}
		detected, err := change.New(s.ids.NewID(), target, values, nil, now)
		if err != nil {
			return nil, fmt.Errorf("build baseline change: %w", err)
		}
		return s.publish(ctx, detected)
	default:
		return nil, fmt.Errorf("load previous snapshot: %w", err)
	}

	added, removed := record.DiffValues(previous.Values, values)
	if len(added) == 0 && len(removed) == 0 {
		return nil, nil
	}

	if err := s.snapshots.Save(ctx, record.Snapshot{
		ID:         s.ids.NewID(),
		TargetID:   target.ID,
		Values:     values,
		TTL:        resolution.TTL,
		ObservedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("save snapshot: %w", err)
	}

	detected, err := change.New(s.ids.NewID(), target, added, removed, now)
	if err != nil {
		return nil, fmt.Errorf("build change: %w", err)
	}
	return s.publish(ctx, detected)
}

func (s *Service) publish(ctx context.Context, detected change.Change) (*change.Change, error) {
	if err := s.changes.Save(ctx, detected); err != nil {
		return nil, fmt.Errorf("save change: %w", err)
	}
	s.logger.Info("dns change detected",
		slog.String("target", detected.TargetName),
		slog.String("type", string(detected.RecordType)),
		slog.Any("added", detected.Added),
		slog.Any("removed", detected.Removed))

	event := change.RecordedEvent{Change: detected, Timestamp: detected.DetectedAt}
	if err := s.bus.Publish(ctx, event); err != nil {
		return nil, fmt.Errorf("publish change event: %w", err)
	}
	return &detected, nil
}
