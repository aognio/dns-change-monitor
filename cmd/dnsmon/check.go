package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/aognio/dns-change-monitor/pkg/app"
	"github.com/aognio/dns-change-monitor/pkg/config"
	"github.com/aognio/dns-change-monitor/pkg/domain/record"
)

// checkRecords resolves the monitored records once and compares the answers
// with the last observation, without recording anything.
func checkRecords(args []string) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
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

	logger, closer, err := newDaemonLogger(cfg)
	if err != nil {
		return err
	}
	defer closer.Close()

	application, err := app.Open(cfg, logger)
	if err != nil {
		return err
	}
	defer application.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results, err := application.Check(ctx)
	if err != nil {
		return err
	}

	failures := 0
	for _, result := range results {
		switch result.State {
		case app.CheckMissing:
			fmt.Printf("%-40s %-6s %s\n", result.Target.Name, result.Target.Type, result.Error)
			failures++
		case app.CheckNew:
			fmt.Printf("%-40s %-6s %s (first observation)\n", result.Target.Name, result.Target.Type, joinValues(result.Values))
		case app.CheckChanged:
			fmt.Printf("%-40s %-6s %s (changed)\n", result.Target.Name, result.Target.Type, joinValues(result.Values))
		default:
			fmt.Printf("%-40s %-6s %s\n", result.Target.Name, result.Target.Type, joinValues(result.Values))
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d record(s) could not be resolved", failures)
	}
	return nil
}

// joinValues renders the answers of a record.
func joinValues(values []string) string {
	if len(values) == 0 {
		return "(no answer)"
	}
	rendered := ""
	for index, value := range values {
		if index > 0 {
			rendered += ", "
		}
		rendered += value
	}
	return rendered
}

// Compile time check that the record type is reachable.
var _ record.Type = record.TypeA
