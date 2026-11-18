// Package shared holds the primitives every domain package depends on:
// clocks, identifier generation, domain events and the errors that
// describe rule violations.
package shared

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Clock hands the domain an injectable source of time so that rules can be
// exercised deterministically in tests.
type Clock interface {
	Now() time.Time
}

// SystemClock is the production clock. It always reports UTC.
type SystemClock struct{}

// Now returns the current UTC time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// IDGenerator produces opaque identifiers for aggregates and entities.
type IDGenerator interface {
	NewID() string
}

// UUIDGenerator produces RFC 4122 version 4 identifiers.
type UUIDGenerator struct{}

// NewID returns a freshly generated UUIDv4 string.
func (UUIDGenerator) NewID() string { return uuid.NewString() }

// Event is a statement that something meaningful happened in the domain.
type Event interface {
	// Name is the routing key subscribers use to listen for this event.
	Name() string
	// OccurredAt is the moment the event came into being.
	OccurredAt() time.Time
}

// EventHandler reacts to a published event.
type EventHandler func(ctx context.Context, event Event) error

// EventBus is the domain's outbound port for broadcasting events. The
// in-process implementation lives in the infrastructure layer; the domain
// never depends on it directly.
type EventBus interface {
	// Publish delivers an event to every subscriber registered for its name.
	Publish(ctx context.Context, event Event) error
	// Subscribe registers a handler for an event name.
	Subscribe(eventName string, handler EventHandler)
}

// Rule violations surfaced by the domain.
var (
	// ErrNotFound signals that an entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists signals a uniqueness constraint was violated.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInvalid signals a value or entity violates an invariant.
	ErrInvalid = errors.New("invalid")
	// ErrUnauthorized signals the actor is not allowed to perform an action.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrExpired signals a time-bounded entity is no longer valid.
	ErrExpired = errors.New("expired")
	// ErrConsumed signals a one-shot entity has already been used.
	ErrConsumed = errors.New("consumed")
)
