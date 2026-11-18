package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/aognio/dns-change-monitor/pkg/app"
	"github.com/aognio/dns-change-monitor/pkg/config"
	"github.com/aognio/dns-change-monitor/pkg/domain/record"
)

// runRecords dispatches the record commands.
func runRecords(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: dnsmon records <list|add|remove>")
	}
	switch args[0] {
	case "list":
		return listRecords(args[1:])
	case "add":
		return addRecord(args[1:])
	case "remove":
		return removeRecord(args[1:])
	default:
		return fmt.Errorf("unknown records command %q", args[0])
	}
}

// listRecords prints the monitored DNS names.
func listRecords(args []string) error {
	flags := flag.NewFlagSet("records list", flag.ContinueOnError)
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

	targets, err := application.Repositories().Targets.List(ctx)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		fmt.Println("no DNS record is monitored yet")
		return nil
	}

	fmt.Printf("%-40s %-8s %-22s %-10s %-24s %s\n", "NAME", "TYPE", "RESOLVER", "STATE", "MODE", "SOURCE")
	for _, target := range targets {
		state := "paused"
		if target.Enabled {
			state = "active"
		}
		resolver := target.Resolver
		if resolver == "" {
			resolver = "(default)"
		}
		fmt.Printf("%-40s %-8s %-22s %-10s %-24s %s\n",
			target.Name, target.Type, resolver, state, describeMode(target.Check), target.Source)
	}
	return nil
}

// describeMode renders the check mode of a target with its expectation.
func describeMode(check record.Check) string {
	mode := string(check.EffectiveMode())
	if len(check.Expect) == 0 {
		return mode
	}
	return mode + " [" + strings.Join(check.Expect, ", ") + "]"
}

// addRecord adds a monitored DNS name.
func addRecord(args []string) error {
	flags := flag.NewFlagSet("records add", flag.ContinueOnError)
	resolver := flags.String("resolver", "", "DNS server to query, as host:port")
	recordType := flags.String("type", "A", "record type to monitor")
	mode := flags.String("mode", "change", "check mode: change, any, exact or regex")
	expect := &stringList{}
	flags.Var(expect, "expect", "expected value, repeatable")
	configFile := flags.String("config", "", "path of the configuration file")
	if err := flags.Parse(reorderArgs(flags, args)); err != nil {
		return err
	}
	if flags.NArg() < 1 || flags.NArg() > 2 {
		return fmt.Errorf("usage: dnsmon records add <name> [type] [--resolver host:port] [--mode mode] [--expect value]")
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

	recordTypeName := *recordType
	if flags.NArg() == 2 {
		recordTypeName = flags.Arg(1)
	}
	parsedType, err := record.ParseType(recordTypeName)
	if err != nil {
		return err
	}
	server := *resolver
	if server == "" && len(cfg.Monitor.Resolvers) > 0 {
		server = cfg.Monitor.Resolvers[0]
	}

	target, err := record.NewTarget(flags.Arg(0), parsedType, server, record.SourceManual, application.Clock().Now())
	if err != nil {
		return err
	}
	target.ID = application.IDs().NewID()

	check, err := record.NewCheck(record.Mode(*mode), expect.values)
	if err != nil {
		return err
	}
	target.Check = check

	existing, err := application.Repositories().Targets.List(ctx)
	if err != nil {
		return err
	}
	for _, candidate := range existing {
		if candidate.Identity() == target.Identity() {
			target.ID = candidate.ID
			target.CreatedAt = candidate.CreatedAt
			break
		}
	}

	if err := application.Repositories().Targets.Upsert(ctx, target); err != nil {
		return err
	}
	fmt.Printf("the record %s %s is now monitored\n", target.Name, target.Type)
	return nil
}

// removeRecord removes a monitored DNS name.
func removeRecord(args []string) error {
	flags := flag.NewFlagSet("records remove", flag.ContinueOnError)
	recordType := flags.String("type", "A", "record type of the record to remove")
	configFile := flags.String("config", "", "path of the configuration file")
	if err := flags.Parse(reorderArgs(flags, args)); err != nil {
		return err
	}
	if flags.NArg() < 1 || flags.NArg() > 2 {
		return fmt.Errorf("usage: dnsmon records remove <name> [type]")
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

	recordTypeName := *recordType
	if flags.NArg() == 2 {
		recordTypeName = flags.Arg(1)
	}
	parsedType, err := record.ParseType(recordTypeName)
	if err != nil {
		return err
	}
	name, err := record.CanonicalName(flags.Arg(0))
	if err != nil {
		return err
	}

	targets, err := application.Repositories().Targets.List(ctx)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.Name == name && target.Type == parsedType {
			if err := application.Repositories().Targets.Delete(ctx, target.ID); err != nil {
				return err
			}
			fmt.Printf("the record %s %s is no longer monitored\n", target.Name, target.Type)
			return nil
		}
	}
	return fmt.Errorf("the record %s %s is not monitored", name, parsedType)
}

// stringList collects the values of a repeatable flag.
type stringList struct {
	values []string
}

// String implements flag.Value.
func (l *stringList) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(l.values, ",")
}

// Set implements flag.Value.
func (l *stringList) Set(value string) error {
	l.values = append(l.values, value)
	return nil
}
