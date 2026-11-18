// Package subscriber models the Telegram accounts that may be notified about
// DNS changes.
package subscriber

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
)

// EventPaired is the name under which a successful pairing is published.
const EventPaired = "subscriber.paired"

// Status describes how far along the pairing process an account is.
type Status string

// The lifecycle states of a subscriber.
const (
	// StatusPending means the account is allowed but has not authorized the
	// bot with a pairing token yet.
	StatusPending Status = "pending"
	// StatusPaired means the account presented a valid pairing token.
	StatusPaired Status = "paired"
	// StatusDisabled means the account must no longer be notified.
	StatusDisabled Status = "disabled"
)

// Subscriber is a Telegram account entitled to receive change notifications.
type Subscriber struct {
	ID             string
	Username       string
	TelegramUserID int64
	ChatID         int64
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
	PairedAt       *time.Time
}

// New builds a pending subscriber for a username.
func New(id string, username string, now time.Time) (Subscriber, error) {
	canonical, err := CanonicalUsername(username)
	if err != nil {
		return Subscriber{}, err
	}
	return Subscriber{
		ID:             id,
		Username:       canonical,
		TelegramUserID: 0,
		ChatID:         0,
		Status:         StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// CanonicalUsername normalises a Telegram username: lower case, no leading @.
func CanonicalUsername(username string) (string, error) {
	trimmed := strings.TrimSpace(username)
	trimmed = strings.TrimPrefix(trimmed, "@")
	trimmed = strings.ToLower(trimmed)
	if trimmed == "" {
		return "", fmt.Errorf("%w: username must not be empty", shared.ErrInvalid)
	}
	if strings.ContainsAny(trimmed, " \t@") {
		return "", fmt.Errorf("%w: username %q contains invalid characters", shared.ErrInvalid, username)
	}
	return trimmed, nil
}

// IsPaired reports whether the account authorized the bot and can therefore
// be notified.
func (s Subscriber) IsPaired() bool {
	return s.Status == StatusPaired && s.TelegramUserID != 0 && s.ChatID != 0
}

// Pair binds the account to a Telegram identity.
func (s Subscriber) Pair(userID, chatID int64, now time.Time) (Subscriber, error) {
	if userID == 0 || chatID == 0 {
		return Subscriber{}, fmt.Errorf("%w: telegram identity is incomplete", shared.ErrInvalid)
	}
	s.TelegramUserID = userID
	s.ChatID = chatID
	s.Status = StatusPaired
	s.UpdatedAt = now
	s.PairedAt = &now
	return s, nil
}

// PairedEvent announces that a Telegram account authorized the bot.
type PairedEvent struct {
	Subscriber Subscriber
	Timestamp  time.Time
}

// Name implements shared.Event.
func (e PairedEvent) Name() string { return EventPaired }

// OccurredAt implements shared.Event.
func (e PairedEvent) OccurredAt() time.Time { return e.Timestamp }

// Repository stores subscribers.
type Repository interface {
	// List returns every subscriber.
	List(ctx context.Context) ([]Subscriber, error)
	// Get returns a subscriber by identifier.
	Get(ctx context.Context, id string) (Subscriber, error)
	// FindByUsername returns the subscriber with the given username.
	FindByUsername(ctx context.Context, username string) (Subscriber, error)
	// FindByTelegramUserID returns the subscriber owning a Telegram account.
	FindByTelegramUserID(ctx context.Context, telegramUserID int64) (Subscriber, error)
	// Upsert stores a subscriber.
	Upsert(ctx context.Context, subscriber Subscriber) error
	// Delete removes a subscriber by identifier.
	Delete(ctx context.Context, id string) error
}
