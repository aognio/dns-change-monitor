// Package telegram adapts the Telegram bot API to the ports declared by the
// domain.
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/domain/notification"
	domaintelegram "github.com/aognio/dns-change-monitor/pkg/domain/telegram"
	telebot "gopkg.in/telebot.v3"
)

// Gateway consumes updates from Telegram and exposes them as commands.
type Gateway struct {
	bot     *telebot.Bot
	logger  *slog.Logger
	timeout time.Duration
	backoff time.Duration

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	commands  chan domaintelegram.Command
}

// Options configure the gateway.
type Options struct {
	// Token is the bot token issued by BotFather.
	Token string
	// PollTimeout is how long a long poll request waits for updates.
	PollTimeout time.Duration
	// APIURL overrides the address of the Telegram API. It is empty in
	// production and points at a local server in the tests.
	APIURL string
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
		Token: options.Token,
		URL:   options.APIURL,
		// The client outlives a single long poll, so its deadline is the
		// poll timeout plus the time the answer may take to travel back.
		Client: &http.Client{Timeout: timeout + 30*time.Second},
		OnError: func(err error, _ telebot.Context) {
			logger.Error("telegram update failed", slog.String("error", err.Error()))
		},
	})
	if err != nil {
		return nil, fmt.Errorf("connect to telegram: %w", err)
	}
	return &Gateway{
		bot:      bot,
		logger:   logger,
		timeout:  timeout,
		backoff:  5 * time.Second,
		stop:     make(chan struct{}),
		commands: make(chan domaintelegram.Command, 64),
	}, nil
}

// Listen implements telegram.Gateway.
//
// The polling of the Telegram API starts once and runs in the background for
// the whole life of the gateway. Every call returns its own stream of
// commands, closed when the context is cancelled.
func (g *Gateway) Listen(ctx context.Context) (<-chan domaintelegram.Command, error) {
	g.startOnce.Do(func() { go g.poll() })

	outgoing := make(chan domaintelegram.Command, 16)
	go func() {
		defer close(outgoing)
		for {
			select {
			case <-ctx.Done():
				return
			case <-g.stop:
				return
			case command, ok := <-g.commands:
				if !ok {
					return
				}
				select {
				case outgoing <- command:
				case <-ctx.Done():
					return
				case <-g.stop:
					return
				}
			}
		}
	}()
	return outgoing, nil
}

// Stop shuts the polling down.
func (g *Gateway) Stop() {
	g.stopOnce.Do(func() { close(g.stop) })
}

// poll asks the Telegram API for the updates until the gateway is stopped.
func (g *Gateway) poll() {
	offset := 0
	for {
		select {
		case <-g.stop:
			return
		default:
		}

		updates, err := g.fetch(offset)
		if err != nil {
			select {
			case <-g.stop:
				return
			default:
			}
			g.logger.Error("telegram poll failed", slog.String("error", err.Error()))
			select {
			case <-g.stop:
				return
			case <-time.After(g.backoff):
			}
			continue
		}

		for _, update := range updates {
			if update.ID >= offset {
				offset = update.ID + 1
			}
			command, ok := g.command(update)
			if !ok {
				continue
			}
			select {
			case g.commands <- command:
			case <-g.stop:
				return
			}
		}
	}
}

// fetch asks the Telegram API for the updates that come after the given one.
func (g *Gateway) fetch(offset int) ([]telebot.Update, error) {
	params := map[string]any{
		"offset":          offset,
		"timeout":         int(g.timeout.Seconds()),
		"allowed_updates": []string{"message"},
	}
	data, err := g.bot.Raw("getUpdates", params)
	if err != nil {
		return nil, err
	}
	var response struct {
		Result []telebot.Update
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode telegram updates: %w", err)
	}
	return response.Result, nil
}

// command turns an update into a domain command, discarding the ones that
// are not meant for this bot.
func (g *Gateway) command(update telebot.Update) (domaintelegram.Command, bool) {
	message := update.Message
	if message == nil || message.Chat == nil {
		return domaintelegram.Command{}, false
	}
	if addressedToAnotherBot(message, g.bot) {
		return domaintelegram.Command{}, false
	}
	return toCommand(update)
}

// addressedToAnotherBot reports whether the message names a bot other than
// this one.
func addressedToAnotherBot(message *telebot.Message, bot *telebot.Bot) bool {
	fields := strings.Fields(message.Text)
	if len(fields) == 0 {
		return false
	}
	verb := strings.TrimPrefix(fields[0], "/")
	index := strings.Index(verb, "@")
	if index < 0 {
		return false
	}
	name := verb[index+1:]
	if bot == nil || bot.Me == nil || bot.Me.Username == "" {
		return false
	}
	return !strings.EqualFold(name, bot.Me.Username)
}

// toCommand converts a Telegram update into a domain command.
func toCommand(update telebot.Update) (domaintelegram.Command, bool) {
	message := update.Message
	if message == nil || message.Chat == nil || message.Text == "" {
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
