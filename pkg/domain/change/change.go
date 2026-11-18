// Package change models a detected difference between two consecutive
// resolutions of a monitored DNS record.
package change

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gnrfan/dns-change-monitor/pkg/domain/record"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/shared"
)

// EventRecorded is the name under which newly detected changes are
// published on the event bus.
const EventRecorded = "change.recorded"

// Change is a single detected difference in the answer set of a target.
type Change struct {
	ID         string
	TargetID   string
	TargetName string
	RecordType record.Type
	Added      []string
	Removed    []string
	DetectedAt time.Time
	NotifiedAt *time.Time
}

// New builds a change from a target and a diff.
func New(id string, target record.Target, added, removed []string, detectedAt time.Time) (Change, error) {
	if id == "" {
		return Change{}, fmt.Errorf("%w: change identifier must not be empty", shared.ErrInvalid)
	}
	if len(added) == 0 && len(removed) == 0 {
		return Change{}, fmt.Errorf("%w: change must carry at least one difference", shared.ErrInvalid)
	}
	return Change{
		ID:         id,
		TargetID:   target.ID,
		TargetName: target.Name,
		RecordType: target.Type,
		Added:      record.NormaliseValues(added),
		Removed:    record.NormaliseValues(removed),
		DetectedAt: detectedAt,
	}, nil
}

// IsEmpty reports whether the change carries no differences at all.
func (c Change) IsEmpty() bool { return len(c.Added) == 0 && len(c.Removed) == 0 }

// IsNotified reports whether subscribers have already been told about it.
func (c Change) IsNotified() bool { return c.NotifiedAt != nil }

// RecordedEvent announces a newly detected change.
type RecordedEvent struct {
	Change    Change
	Timestamp time.Time
}

// Name implements shared.Event.
func (e RecordedEvent) Name() string { return EventRecorded }

// OccurredAt implements shared.Event.
func (e RecordedEvent) OccurredAt() time.Time { return e.Timestamp }

// Repository stores detected changes.
type Repository interface {
	// Save stores a change.
	Save(ctx context.Context, change Change) error
	// MarkNotified records that subscribers have been informed.
	MarkNotified(ctx context.Context, id string, at time.Time) error
	// ListPending returns changes that have not been reported yet.
	ListPending(ctx context.Context) ([]Change, error)
	// List returns the most recent changes, newest first.
	List(ctx context.Context, limit int) ([]Change, error)
}

// Summary renders a short, human readable description of the change.
func (c Change) Summary() string {
	parts := make([]string, 0, 2)
	if len(c.Added) > 0 {
		parts = append(parts, "+"+strings.Join(c.Added, ", "))
	}
	if len(c.Removed) > 0 {
		parts = append(parts, "-"+strings.Join(c.Removed, ", "))
	}
	return fmt.Sprintf("%s %s (%s)", c.TargetName, c.RecordType, strings.Join(parts, " "))
}
