// Package telegram adapts the Telegram bot API to the ports declared by the
// domain.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gnrfan/dns-change-monitor/pkg/domain/notification"
	domaintelegram "github.com/gnrfan/dns-change-monitor/pkg/domain/telegram"
	telebot "gopkg.in/telebot.v3"
)

// Gateway consumes updates from Telegram and exposes them as commands.
type Gateway struct {
	bot    *telebot.Bot
	logger *slog.Logger
	once   sync.Once
}

// Options configure the gateway.
type Options struct {
	// Token is the bot token issued by BotFather.
	Token string
	// PollTimeout is how long a long poll request waits for updates.
	PollTimeout time.Duration
	// Logger receives the diagnostics of the adapter.
	Logger *slog.Logger
}

// New builds a gateway bound to a Telegram bot.
func New(options Options) (*Gateway, error) {
	if strings.TrimSpace(options.Token) == "" {
		return nil, fmt.Errorf("telegram bot token is empty")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := options.PollTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	bot, err := telebot.NewBot(telebot.Settings{
		Token:  options.Token,
		Poller: &telebot.LongPoller{Timeout: timeout},
		OnError: func(err error, _ telebot.Context) {
			logger.Error("telegram update failed", slog.String("error", err.Error()))
		},
	})
	if err != nil {
		return nil, fmt.Errorf("connect to telegram: %w", err)
	}
	return &Gateway{bot: bot, logger: logger}, nil
}

// Listen implements telegram.Gateway.
func (g *Gateway) Listen(ctx context.Context) (<-chan domaintelegram.Command, error) {
	g.once.Do(func() { g.bot.Start() })

	commands := make(chan domaintelegram.Command, 32)
	go func() {
		defer close(commands)
		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-g.bot.Updates:
				if !ok {
					return
				}
				command, ok := toCommand(update)
				if !ok {
					continue
				}
				select {
				case commands <- command:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return commands, nil
}

// Stop shuts the bot down.
func (g *Gateway) Stop() {
	g.bot.Stop()
}

// toCommand converts a Telegram update into a domain command.
func toCommand(update telebot.Update) (domaintelegram.Command, bool) {
	message := update.Message
	if message == nil || message.Text == "" {
		return domaintelegram.Command{}, false
	}
	if !strings.HasPrefix(message.Text, "/") {
		return domaintelegram.Command{}, false
	}

	fields := strings.Fields(message.Text)
	if len(fields) == 0 {
		return domaintelegram.Command{}, false
	}

	verb := strings.TrimPrefix(fields[0], "/")
	if index := strings.Index(verb, "@"); index >= 0 {
		verb = verb[:index]
	}
	verb = strings.ToLower(verb)

	payload := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(message.Text), fields[0]))

	command := domaintelegram.Command{
		ChatID:  message.Chat.ID,
		Command: verb,
		Payload: payload,
	}
	if message.Sender != nil {
		command.UserID = message.Sender.ID
		command.Username = strings.TrimPrefix(message.Sender.Username, "@")
		command.FirstName = message.Sender.FirstName
	}
	return command, true
}

// Notifier delivers messages through the Telegram bot.
type Notifier struct {
	bot    *telebot.Bot
	logger *slog.Logger
}

// NewNotifier builds a notifier sharing the bot of a gateway.
func NewNotifier(gateway *Gateway, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{bot: gateway.bot, logger: logger}
}

// Send implements notification.Notifier.
func (n *Notifier) Send(_ context.Context, recipient notification.Recipient, message notification.Message) error {
	target := ""
	if recipient.ChatID != 0 {
		target = formatChatID(recipient.ChatID)
	} else {
		target = recipient.Username
	}
	if target == "" {
		return fmt.Errorf("no recipient for notification")
	}

	if _, err := n.bot.Send(&chatRecipient{value: target}, message.Text); err != nil {
		return fmt.Errorf("send telegram message to %s: %w", target, err)
	}
	return nil
}

// chatRecipient adapts a chat identifier or a username to telebot.
type chatRecipient struct {
	value string
}

// Recipient implements telebot.Recipient.
func (c *chatRecipient) Recipient() string { return c.value }

// String renders the recipient for logs.
func (c *chatRecipient) String() string { return c.value }

// formatChatID renders a numeric chat identifier.
func formatChatID(id int64) string { return strconv.FormatInt(id, 10) }
