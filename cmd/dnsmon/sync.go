package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/aognio/dns-change-monitor/pkg/app"
	"github.com/aognio/dns-change-monitor/pkg/config"
)

// syncConfiguration applies the configuration file to the database without
// starting the monitor. The records declared in the file become monitored
// targets and the usernames allowed to use the bot become subscribers
// waiting for a pairing token.
func syncConfiguration(args []string) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
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

	if err := application.Seed(ctx); err != nil {
		return fmt.Errorf("apply the configuration: %w", err)
	}

	targets, err := application.Repositories().Targets.List(ctx)
	if err != nil {
		return err
	}
	subscribers, err := application.Repositories().Subscribers.List(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("%d monitored record(s) and %d subscriber(s) in %s\n",
		len(targets), len(subscribers), cfg.Database.Path)
	for _, target := range targets {
		state := "paused"
		if target.Enabled {
			state = "active"
		}
		fmt.Printf("  %-40s %-6s %-8s %s\n", target.Name, target.Type, state, target.Source)
	}
	return nil
}
