package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/gnrfan/dns-change-monitor/pkg/app"
	"github.com/gnrfan/dns-change-monitor/pkg/config"
	"github.com/gnrfan/dns-change-monitor/pkg/infra/daemon"
)

// runMonitor starts the monitor, either in the foreground or as a daemon.
func runMonitor(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	daemonize := flags.Bool("daemon", false, "run the monitor in the background")
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

	if *daemonize && !daemon.IsDaemonized() {
		return startDaemon(cfg, path)
	}
	return runForeground(cfg)
}

// startDaemon forks a background copy of the monitor.
func startDaemon(cfg *config.Config, configPath string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate the executable: %w", err)
	}
	resolved, err := filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve the executable path: %w", err)
	}

	pid, err := daemon.Spawn(resolved, []string{"run", "--config", configPath}, cfg.Daemon.LogFile, cfg.Daemon.PIDFile)
	if err != nil {
		return err
	}
	fmt.Printf("the monitor runs in the background with the process identifier %d\n", pid)
	fmt.Printf("logs are written to %s\n", cfg.Daemon.LogFile)
	return nil
}

// runForeground runs the monitor in the current process.
func runForeground(cfg *config.Config) error {
	logger, err := newLogger(cfg)
	if err != nil {
		return err
	}

	application, err := app.Open(cfg, logger)
	if err != nil {
		return err
	}
	defer application.Close()

	ctx, cancel := notifyContext()
	defer cancel()

	if err := application.Seed(ctx); err != nil {
		return fmt.Errorf("prepare the monitored records: %w", err)
	}
	if err := application.Assemble(); err != nil {
		return fmt.Errorf("assemble the agents: %w (check the telegram.token of the configuration file)", err)
	}

	if daemon.IsDaemonized() {
		if err := daemon.WritePIDFile(cfg.Daemon.PIDFile); err != nil {
			return err
		}
		defer daemon.RemovePIDFile(cfg.Daemon.PIDFile)
	}

	logger.Info("the monitor is running",
		slog.String("database", cfg.Database.Path),
		slog.Duration("interval", cfg.Interval()),
		slog.Int("telegram_accounts", len(cfg.Usernames())))

	if err := application.Run(ctx); err != nil && ctx.Err() == nil {
		return err
	}
	logger.Info("the monitor stopped")
	return nil
}

// stopDaemon stops the running daemon.
func stopDaemon(args []string) error {
	flags := flag.NewFlagSet("stop", flag.ContinueOnError)
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

	pid, err := daemon.ReadPIDFile(cfg.Daemon.PIDFile)
	if err != nil {
		return fmt.Errorf("the monitor does not appear to be running: %w", err)
	}
	if !daemon.IsRunning(pid) {
		daemon.RemovePIDFile(cfg.Daemon.PIDFile)
		return fmt.Errorf("the monitor does not appear to be running")
	}
	if err := daemon.Stop(pid); err != nil {
		return err
	}
	fmt.Printf("the monitor with the process identifier %d was asked to stop\n", pid)
	return nil
}

// showStatus prints the state of the monitor.
func showStatus(args []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
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

	summary, err := application.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Print(summary)
	return nil
}

// newLogger builds the logger described by the configuration.
func newLogger(cfg *config.Config) (*slog.Logger, error) {
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	options := &slog.HandlerOptions{Level: level}
	if cfg.Log.Format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, options)), nil
	}
	return slog.New(slog.NewTextHandler(os.Stdout, options)), nil
}
