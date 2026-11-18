// Command dnsmon watches DNS records and reports their changes through a
// Telegram bot.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gnrfan/dns-change-monitor/pkg/build"
	"github.com/gnrfan/dns-change-monitor/pkg/config"
)

const usage = `dnsmon - DNS change monitor with Telegram notifications

Usage:
  dnsmon <command> [options]

Commands:
  run           Run the monitor, in the foreground or as a daemon
  stop          Stop the running daemon
  status        Show the state of the monitor
  sync          Apply the configuration file to the database
  check         Resolve the monitored records once and show the answers
  token create  Mint a pairing token for a Telegram account
  token list    List the pairing tokens
  records       Manage the monitored DNS records
  subscribers   Manage the accounts allowed to receive notifications
  version       Print the version of the binary

Global options:
  --config <path>   Path of the configuration file (default: config.toml)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// globalConfigPath holds the configuration path given before the command.
var globalConfigPath string

// run dispatches a command.
func run(args []string) error {
	globalConfigPath, args = splitGlobalFlag(args)

	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	switch args[0] {
	case "run":
		return runMonitor(args[1:])
	case "stop":
		return stopDaemon(args[1:])
	case "status":
		return showStatus(args[1:])
	case "sync":
		return syncConfiguration(args[1:])
	case "check":
		return checkRecords(args[1:])
	case "token":
		return runToken(args[1:])
	case "records":
		return runRecords(args[1:])
	case "subscribers":
		return runSubscribers(args[1:])
	case "version", "--version", "-v":
		fmt.Println("dnsmon", build.String())
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// notifyContext returns a context cancelled on the usual termination
// signals.
func notifyContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// splitGlobalFlag extracts the configuration path given before the command
// and returns the remaining arguments.
func splitGlobalFlag(args []string) (string, []string) {
	path := ""
	remaining := make([]string, 0, len(args))

	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--config" && index+1 < len(args):
			path = args[index+1]
			index++
		case len(argument) > len("--config=") && argument[:len("--config=")] == "--config=":
			path = argument[len("--config="):]
		default:
			remaining = append(remaining, argument)
		}
	}
	return path, remaining
}

// configPath extracts the path of the configuration file from the arguments.
func configPath(args []string) string {
	for index, arg := range args {
		if arg == "--config" && index+1 < len(args) {
			return args[index+1]
		}
		if len(arg) > len("--config=") && arg[:len("--config=")] == "--config=" {
			return arg[len("--config="):]
		}
	}
	if globalConfigPath != "" {
		return globalConfigPath
	}
	if path := os.Getenv(config.EnvConfigPath); path != "" {
		return path
	}
	return config.DefaultPath
}

// reorderArgs moves the options in front of the positional arguments so that
// they can be given in any order.
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	options := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))

	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "-") || argument == "-" || argument == "--" {
			positional = append(positional, argument)
			continue
		}

		name := strings.TrimLeft(argument, "-")
		if !strings.Contains(name, "=") {
			option := fs.Lookup(name)
			if option != nil && !isBoolOption(option) && index+1 < len(args) {
				index++
				options = append(options, argument, args[index])
				continue
			}
		}
		options = append(options, argument)
	}
	return append(options, positional...)
}

// isBoolOption reports whether an option takes no value.
func isBoolOption(option *flag.Flag) bool {
	bounded, ok := option.Value.(interface{ IsBoolFlag() bool })
	return ok && bounded.IsBoolFlag()
}
