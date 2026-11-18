// Package pairer holds the agent that authorizes Telegram accounts to talk to
// the bot.
package pairer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	apppairing "github.com/aognio/dns-change-monitor/pkg/app/pairing"
	"github.com/aognio/dns-change-monitor/pkg/domain/notification"
	domaintelegram "github.com/aognio/dns-change-monitor/pkg/domain/telegram"
)

// StatusProvider renders the state of the monitor for the status command.
type StatusProvider interface {
	// Status returns a human readable summary of the monitor.
	Status(ctx context.Context) (string, error)
}

// Agent consumes the commands sent to the bot.
type Agent struct {
	gateway  domaintelegram.Gateway
	service  *apppairing.Service
	notifier notification.Notifier
	status   StatusProvider
	logger   *slog.Logger
}

// Options configure the pairing agent.
type Options struct {
	// Gateway delivers the incoming commands.
	Gateway domaintelegram.Gateway
	// Service redeems the pairing tokens.
	Service *apppairing.Service
	// Notifier sends the replies back to Telegram.
	Notifier notification.Notifier
	// Status renders the state of the monitor.
	Status StatusProvider
	// Logger receives the diagnostics of the agent.
	Logger *slog.Logger
}

// New builds the pairing agent.
func New(options Options) *Agent {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Agent{
		gateway:  options.Gateway,
		service:  options.Service,
		notifier: options.Notifier,
		status:   options.Status,
		logger:   logger,
	}
}

// Name implements agent.Agent.
func (a *Agent) Name() string { return "telegram-pairer" }

// Goal implements agent.Agent.
func (a *Agent) Goal() string {
	return "authorize Telegram accounts with one-shot pairing tokens"
}

// Run implements agent.Agent.
func (a *Agent) Run(ctx context.Context) error {
	commands, err := a.gateway.Listen(ctx)
	if err != nil {
		return fmt.Errorf("listen for telegram commands: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case command, ok := <-commands:
			if !ok {
				return nil
			}
			a.handle(ctx, command)
		}
	}
}

// handle dispatches a command and answers the sender.
func (a *Agent) handle(ctx context.Context, command domaintelegram.Command) {
	switch command.Command {
	case "start":
		a.handleStart(ctx, command)
	case "status":
		a.handleStatus(ctx, command)
	case "help":
		a.reply(ctx, command, usage())
	default:
		a.reply(ctx, command, "Unknown command. Send "+usage())
	}
}

// handleStart redeems the pairing token carried by the command.
func (a *Agent) handleStart(ctx context.Context, command domaintelegram.Command) {
	if strings.TrimSpace(command.Payload) == "" {
		a.reply(ctx, command, "The bot is running and answers to commands.\n"+
			"Send the pairing token with the command:\n/start <token>")
		return
	}

	outcome, err := a.service.Pair(ctx, command)
	if err != nil {
		a.logger.Error("pairing failed",
			slog.String("username", command.Username),
			slog.String("error", err.Error()))
		a.reply(ctx, command, "The pairing could not be completed, please try again later.")
		return
	}

	if !outcome.Paired {
		a.reply(ctx, command, "The pairing was refused: "+outcome.Reason+".")
		return
	}
	a.reply(ctx, command, fmt.Sprintf(
		"Account %s is now authorized. This daemon reports DNS changes to you as soon as they are live.",
		outcome.Username))
}

// handleStatus answers with the state of the monitor.
func (a *Agent) handleStatus(ctx context.Context, command domaintelegram.Command) {
	if a.status == nil {
		a.reply(ctx, command, "The status is not available.")
		return
	}
	summary, err := a.status.Status(ctx)
	if err != nil {
		a.reply(ctx, command, "The status could not be read.")
		return
	}
	a.reply(ctx, command, summary)
}

// reply sends a message back to the sender.
func (a *Agent) reply(ctx context.Context, command domaintelegram.Command, text string) {
	if a.notifier == nil {
		return
	}
	recipient := notification.Recipient{ChatID: command.ChatID, Username: command.Username}
	if err := a.notifier.Send(ctx, recipient, notification.Message{Text: text}); err != nil {
		a.logger.Error("reply not delivered",
			slog.String("username", command.Username),
			slog.String("error", err.Error()))
	}
}

// usage describes the commands the bot understands.
func usage() string {
	return strings.Join([]string{
		"Available commands:",
		"/start <token> - authorize this account with a pairing token",
		"/status - show the state of the monitor",
		"/help - show this message",
	}, "\n")
}
