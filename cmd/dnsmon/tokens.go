package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/app"
	"github.com/aognio/dns-change-monitor/pkg/app/pairing"
	"github.com/aognio/dns-change-monitor/pkg/config"
)

// runToken dispatches the pairing token commands.
func runToken(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: dnsmon token <create|list>")
	}
	switch args[0] {
	case "create":
		return createToken(args[1:])
	case "list":
		return listTokens(args[1:])
	default:
		return fmt.Errorf("unknown token command %q", args[0])
	}
}

// createToken mints a pairing token.
func createToken(args []string) error {
	flags := flag.NewFlagSet("token create", flag.ContinueOnError)
	ttlMinutes := flags.Int("ttl", 0, "lifetime of the token in minutes")
	createdBy := flags.String("by", "cli", "name of the operator asking for the token")
	configFile := flags.String("config", "", "path of the configuration file")
	if err := flags.Parse(reorderArgs(flags, args)); err != nil {
		return err
	}

	path := *configFile
	if path == "" {
		path = configPath(args)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg)
	if err != nil {
		return err
	}

	application, err := app.Open(cfg, logger)
	if err != nil {
		return err
	}
	defer application.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ttl := cfg.TokenTTL()
	if *ttlMinutes > 0 {
		ttl = time.Duration(*ttlMinutes) * time.Minute
	}

	service := pairing.New(
		application.Repositories().Tokens,
		application.Repositories().Subscribers,
		cfg.Usernames(),
		application.IDs(),
		application.Clock(),
		logger,
	)

	token, err := service.Issue(ctx, ttl, *createdBy)
	if err != nil {
		return err
	}

	fmt.Println("Pairing token:", token.ID)
	fmt.Println("Valid until  :", token.ExpiresAt.Local().Format(time.RFC3339))
	fmt.Println("Send to the bot: /start", token.ID)
	return nil
}

// listTokens prints the pairing tokens.
func listTokens(args []string) error {
	flags := flag.NewFlagSet("token list", flag.ContinueOnError)
	limit := flags.Int("limit", 20, "number of tokens to show")
	configFile := flags.String("config", "", "path of the configuration file")
	if err := flags.Parse(reorderArgs(flags, args)); err != nil {
		return err
	}

	path := *configFile
	if path == "" {
		path = configPath(args)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg)
	if err != nil {
		return err
	}

	application, err := app.Open(cfg, logger)
	if err != nil {
		return err
	}
	defer application.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tokens, err := application.Repositories().Tokens.List(ctx, *limit)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		fmt.Println("no pairing token has been minted yet")
		return nil
	}

	now := application.Clock().Now()
	fmt.Printf("%-38s %-20s %-12s %s\n", "TOKEN", "CREATED", "STATE", "EXPIRES")
	for _, token := range tokens {
		state := "usable"
		switch {
		case token.IsConsumed():
			state = "used"
		case token.IsExpired(now):
			state = "expired"
		}
		fmt.Printf("%-38s %-20s %-12s %s\n",
			token.ID,
			token.CreatedAt.Local().Format("2006-01-02 15:04"),
			state,
			token.ExpiresAt.Local().Format("2006-01-02 15:04"))
	}
	return nil
}

// runSubscribers dispatches the subscriber commands.
func runSubscribers(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: dnsmon subscribers <list|remove>")
	}
	switch args[0] {
	case "list":
		return listSubscribers(args[1:])
	case "remove":
		return removeSubscriber(args[1:])
	default:
		return fmt.Errorf("unknown subscribers command %q", args[0])
	}
}

// listSubscribers prints the accounts allowed to receive notifications.
func listSubscribers(args []string) error {
	flags := flag.NewFlagSet("subscribers list", flag.ContinueOnError)
	configFile := flags.String("config", "", "path of the configuration file")
	if err := flags.Parse(reorderArgs(flags, args)); err != nil {
		return err
	}

	path := *configFile
	if path == "" {
		path = configPath(args)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg)
	if err != nil {
		return err
	}

	application, err := app.Open(cfg, logger)
	if err != nil {
		return err
	}
	defer application.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subscribers, err := application.Repositories().Subscribers.List(ctx)
	if err != nil {
		return err
	}
	if len(subscribers) == 0 {
		fmt.Println("no account is registered")
		return nil
	}

	fmt.Printf("%-24s %-12s %-12s %s\n", "USERNAME", "STATE", "TELEGRAM ID", "PAIRED AT")
	for _, entry := range subscribers {
		pairedAt := "-"
		if entry.PairedAt != nil {
			pairedAt = entry.PairedAt.Local().Format("2006-01-02 15:04")
		}
		telegramID := "-"
		if entry.TelegramUserID != 0 {
			telegramID = fmt.Sprintf("%d", entry.TelegramUserID)
		}
		fmt.Printf("%-24s %-12s %-12s %s\n", entry.Username, entry.Status, telegramID, pairedAt)
	}
	return nil
}

// removeSubscriber removes an account.
func removeSubscriber(args []string) error {
	flags := flag.NewFlagSet("subscribers remove", flag.ContinueOnError)
	configFile := flags.String("config", "", "path of the configuration file")
	if err := flags.Parse(reorderArgs(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: dnsmon subscribers remove <username>")
	}

	path := *configFile
	if path == "" {
		path = configPath(args)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg)
	if err != nil {
		return err
	}

	application, err := app.Open(cfg, logger)
	if err != nil {
		return err
	}
	defer application.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	username := strings.TrimPrefix(strings.ToLower(flags.Arg(0)), "@")
	entry, err := application.Repositories().Subscribers.FindByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("no account named %q is registered", username)
	}
	if err := application.Repositories().Subscribers.Delete(ctx, entry.ID); err != nil {
		return err
	}
	fmt.Printf("the account %s was removed\n", username)
	return nil
}
