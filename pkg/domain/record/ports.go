package record

import "context"

// TargetRepository stores the DNS names under observation.
type TargetRepository interface {
	// List returns every stored target.
	List(ctx context.Context) ([]Target, error)
	// Get returns a single target by identifier.
	Get(ctx context.Context, id string) (Target, error)
	// Upsert stores a target, replacing any existing record with the same
	// natural identity.
	Upsert(ctx context.Context, target Target) error
	// Delete removes a target by identifier.
	Delete(ctx context.Context, id string) error
}

// SnapshotRepository stores the last observed answer set of every target.
type SnapshotRepository interface {
	// Latest returns the most recent snapshot of a target. It reports
	// shared.ErrNotFound when the target has never been resolved.
	Latest(ctx context.Context, targetID string) (Snapshot, error)
	// Save stores a snapshot.
	Save(ctx context.Context, snapshot Snapshot) error
}

// Resolver asks DNS servers about a target. Implementations live in the
// infrastructure layer.
type Resolver interface {
	// Resolve returns the current answer set for a target.
	Resolve(ctx context.Context, target Target) (Resolution, error)
}
