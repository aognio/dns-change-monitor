// Package pairing models the one-shot tokens that authorize a Telegram
// account to talk to the bot.
package pairing

import (
	"context"
	"fmt"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
)

// Token is a one-shot authorization credential.
type Token struct {
	ID         string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	ConsumedBy string
	CreatedBy  string
}

// DefaultTTL is how long a freshly minted token stays usable.
const DefaultTTL = 30 * time.Minute

// NewToken builds a token valid for the given duration.
func NewToken(id string, ttl time.Duration, createdBy string, now time.Time) (Token, error) {
	if id == "" {
		return Token{}, fmt.Errorf("%w: token identifier must not be empty", shared.ErrInvalid)
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return Token{
		ID:        id,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
		CreatedBy: createdBy,
	}, nil
}

// IsExpired reports whether the token is past its validity window.
func (t Token) IsExpired(now time.Time) bool { return now.After(t.ExpiresAt) }

// IsConsumed reports whether the token has already been redeemed.
func (t Token) IsConsumed() bool { return t.ConsumedAt != nil }

// IsUsable reports whether the token may still be redeemed.
func (t Token) IsUsable(now time.Time) bool { return !t.IsConsumed() && !t.IsExpired(now) }

// Consume redeems the token for a subscriber.
func (t Token) Consume(now time.Time, subscriberID string) (Token, error) {
	if t.IsConsumed() {
		return Token{}, fmt.Errorf("%w: token %s", shared.ErrConsumed, t.ID)
	}
	if t.IsExpired(now) {
		return Token{}, fmt.Errorf("%w: token %s", shared.ErrExpired, t.ID)
	}
	if subscriberID == "" {
		return Token{}, fmt.Errorf("%w: token requires a subscriber", shared.ErrInvalid)
	}
	t.ConsumedAt = &now
	t.ConsumedBy = subscriberID
	return t, nil
}

// Repository stores pairing tokens.
type Repository interface {
	// Create stores a new token.
	Create(ctx context.Context, token Token) error
	// Get returns a token by identifier.
	Get(ctx context.Context, id string) (Token, error)
	// Save stores an updated token.
	Save(ctx context.Context, token Token) error
	// List returns the most recently created tokens, newest first.
	List(ctx context.Context, limit int) ([]Token, error)
}
