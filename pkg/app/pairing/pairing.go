// Package pairing implements the use case that authorizes Telegram accounts
// to talk to the bot by redeeming a one-shot token.
package pairing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gnrfan/dns-change-monitor/pkg/domain/pairing"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/shared"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/subscriber"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/telegram"
)

// Outcome describes the result of a pairing attempt.
type Outcome struct {
	// Paired reports whether the account was authorized.
	Paired bool
	// Username is the account that attempted the pairing.
	Username string
	// Reason explains a refused pairing.
	Reason string
}

// Service redeems pairing tokens.
type Service struct {
	tokens      pairing.Repository
	subscribers subscriber.Repository
	allowlist   map[string]struct{}
	ids         shared.IDGenerator
	clock       shared.Clock
	logger      *slog.Logger
}

// New builds the pairing service. The allowlist holds the usernames allowed
// to pair; an empty allowlist accepts every account.
func New(
	tokens pairing.Repository,
	subscribers subscriber.Repository,
	allowlist []string,
	ids shared.IDGenerator,
	clock shared.Clock,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	allowed := make(map[string]struct{}, len(allowlist))
	for _, username := range allowlist {
		canonical, err := subscriber.CanonicalUsername(username)
		if err != nil {
			continue
		}
		allowed[canonical] = struct{}{}
	}
	return &Service{
		tokens:      tokens,
		subscribers: subscribers,
		allowlist:   allowed,
		ids:         ids,
		clock:       clock,
		logger:      logger,
	}
}

// Issue mints a new pairing token.
func (s *Service) Issue(ctx context.Context, ttl time.Duration, createdBy string) (pairing.Token, error) {
	now := s.clock.Now()
	token, err := pairing.NewToken(s.ids.NewID(), ttl, createdBy, now)
	if err != nil {
		return pairing.Token{}, err
	}
	if err := s.tokens.Create(ctx, token); err != nil {
		return pairing.Token{}, fmt.Errorf("store pairing token: %w", err)
	}
	return token, nil
}

// Pair redeems the token carried by a command and authorizes the sending
// Telegram account.
func (s *Service) Pair(ctx context.Context, command telegram.Command) (Outcome, error) {
	username, err := subscriber.CanonicalUsername(command.Username)
	if err != nil {
		return Outcome{Reason: "the account has no Telegram username"}, nil
	}
	outcome := Outcome{Username: username}

	payload := strings.TrimSpace(command.Payload)
	if payload == "" {
		outcome.Reason = "no pairing token supplied"
		return outcome, nil
	}

	token, err := s.tokens.Get(ctx, payload)
	if err != nil {
		if isNotFound(err) {
			outcome.Reason = "unknown pairing token"
			return outcome, nil
		}
		return outcome, fmt.Errorf("load pairing token: %w", err)
	}

	now := s.clock.Now()
	if !token.IsUsable(now) {
		if token.IsConsumed() {
			outcome.Reason = "this pairing token has already been used"
		} else {
			outcome.Reason = "this pairing token has expired"
		}
		return outcome, nil
	}

	if !s.allowed(username, command.UserID) {
		outcome.Reason = "this account is not allowed to use the bot"
		return outcome, nil
	}

	target, err := s.subscriberFor(ctx, username)
	if err != nil {
		return outcome, err
	}

	target, err = target.Pair(command.UserID, command.ChatID, now)
	if err != nil {
		return outcome, fmt.Errorf("pair subscriber: %w", err)
	}
	if err := s.subscribers.Upsert(ctx, target); err != nil {
		return outcome, fmt.Errorf("store subscriber: %w", err)
	}

	consumed, err := token.Consume(now, target.ID)
	if err != nil {
		return outcome, fmt.Errorf("consume pairing token: %w", err)
	}
	if err := s.tokens.Save(ctx, consumed); err != nil {
		return outcome, fmt.Errorf("store consumed pairing token: %w", err)
	}

	s.logger.Info("account paired",
		slog.String("username", username),
		slog.Int64("telegram_user_id", command.UserID))

	outcome.Paired = true
	return outcome, nil
}

// subscriberFor finds or creates the subscriber record for a username.
func (s *Service) subscriberFor(ctx context.Context, username string) (subscriber.Subscriber, error) {
	existing, err := s.subscribers.FindByUsername(ctx, username)
	switch {
	case err == nil:
		return existing, nil
	case isNotFound(err):
		created, err := subscriber.New(s.ids.NewID(), username, s.clock.Now())
		if err != nil {
			return subscriber.Subscriber{}, err
		}
		return created, nil
	default:
		return subscriber.Subscriber{}, fmt.Errorf("find subscriber: %w", err)
	}
}

// allowed reports whether an account may pair with the bot.
func (s *Service) allowed(username string, telegramUserID int64) bool {
	if len(s.allowlist) == 0 {
		return true
	}
	if _, ok := s.allowlist[username]; ok {
		return true
	}
	// An account that already paired keeps its access, even when the
	// allowlist changed in the meantime.
	if telegramUserID != 0 {
		if existing, err := s.subscribers.FindByTelegramUserID(context.Background(), telegramUserID); err == nil {
			return existing.IsPaired()
		}
	}
	return false
}

func isNotFound(err error) bool {
	return err != nil && (err == shared.ErrNotFound || strings.Contains(err.Error(), shared.ErrNotFound.Error()))
}
