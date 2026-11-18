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
	violations := 0
	for _, result := range results {
		switch result.State {
		case app.CheckMissing:
			fmt.Printf("%-40s %-6s %s\n", result.Target.Name, result.Target.Type, result.Error)
			failures++
			continue
		default:
			fmt.Printf("%-40s %-6s %s%s\n", result.Target.Name, result.Target.Type,
				joinValues(result.Values), describeVerdict(result))
		}
		if !result.Satisfied {
			violations++
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d record(s) could not be resolved", failures)
	}
	if violations > 0 {
		return fmt.Errorf("%d expectation(s) violated", violations)
	}
	return nil
}

// describeVerdict renders the verdict of the expectation of a record next to
// the state of the answer.
func describeVerdict(result app.CheckResult) string {
	if !result.Target.Check.IsDeclared() {
		return describeState(result.State)
	}
	verdict := result.Verdict()
	return fmt.Sprintf(" (%s, %s)", result.State, verdict)
}

// describeState renders the state of a record declaring no expectation.
func describeState(state app.CheckState) string {
	switch state {
	case app.CheckNew:
		return " (first observation)"
	case app.CheckChanged:
		return " (changed)"
	default:
		return ""
	}
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
