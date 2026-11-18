// Package config loads and validates the TOML configuration that drives the
// monitor.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/record"
	"github.com/gnrfan/dns-change-monitor/pkg/domain/shared"
)

// Environment variables that override file based settings.
const (
	// EnvToken overrides the Telegram bot token.
	EnvToken = "DNSMON_TELEGRAM_TOKEN"
	// EnvConfigPath overrides the path of the configuration file.
	EnvConfigPath = "DNSMON_CONFIG"
)

// Default file locations.
const (
	// DefaultPath is the configuration file looked up when none is given.
	DefaultPath = "config.toml"
)

// Config is the whole configuration of the daemon.
type Config struct {
	Database Database `toml:"database"`
	Telegram Telegram `toml:"telegram"`
	Monitor  Monitor  `toml:"monitor"`
	Pairing  Pairing  `toml:"pairing"`
	Daemon   Daemon   `toml:"daemon"`
	Log      Log      `toml:"log"`
}

// Database points at the SQLite file holding all state.
type Database struct {
	// Path is the location of the SQLite database file.
	Path string `toml:"path"`
}

// Telegram holds the bot credentials and the audience to notify.
type Telegram struct {
	// Token is the bot token issued by BotFather.
	Token string `toml:"token"`
	// NotifyUsernames lists the Telegram accounts allowed to pair and to
	// receive notifications.
	NotifyUsernames []string `toml:"notify_usernames"`
	// PollTimeoutSeconds is how long a long poll request waits for updates.
	PollTimeoutSeconds int `toml:"poll_timeout_seconds"`
}

// Monitor describes how DNS names are watched.
type Monitor struct {
	// IntervalSeconds is the delay between two consecutive checks.
	IntervalSeconds int `toml:"interval_seconds"`
	// Resolvers are the DNS servers used when a target names none.
	Resolvers []string `toml:"resolvers"`
	// TimeoutSeconds bounds a single DNS query.
	TimeoutSeconds int `toml:"timeout_seconds"`
	// NotifyOnBaseline reports the very first observation of a target.
	NotifyOnBaseline bool `toml:"notify_on_baseline"`
	// Records are the DNS names declared in the configuration file.
	Records []Record `toml:"records"`
}

// Record is a DNS name declared in the configuration file.
type Record struct {
	// Name is the DNS name to watch.
	Name string `toml:"name"`
	// Type is the record type to watch.
	Type string `toml:"type"`
	// Resolver is the DNS server to use, as host:port.
	Resolver string `toml:"resolver"`
	// Enabled allows a record to be kept in the file but paused.
	Enabled *bool `toml:"enabled"`
}

// Pairing controls the lifetime of authorization tokens.
type Pairing struct {
	// TokenTTLMinutes is how long a token stays redeemable.
	TokenTTLMinutes int `toml:"token_ttl_minutes"`
}

// Daemon controls background operation.
type Daemon struct {
	// PIDFile holds the process identifier of the background daemon.
	PIDFile string `toml:"pid_file"`
	// LogFile is where the background daemon writes its output.
	LogFile string `toml:"log_file"`
}

// Log controls the logging output.
type Log struct {
	// Level is one of debug, info, warn or error.
	Level string `toml:"level"`
	// Format is either text or json.
	Format string `toml:"format"`
}

// Defaults applied when the file leaves a value unset.
const (
	defaultIntervalSeconds = 60
	defaultTimeoutSeconds  = 5
	defaultPollTimeout     = 30
	defaultTokenTTLMinutes = 30
)

// Load reads the configuration file, applies defaults and validates it.
func Load(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv(EnvConfigPath)
	}
	if path == "" {
		path = DefaultPath
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read configuration %s: %w", path, err)
	}

	cfg := &Config{}
	if err := toml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse configuration %s: %w", path, err)
	}
	if token := os.Getenv(EnvToken); token != "" {
		cfg.Telegram.Token = token
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration %s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Database.Path == "" {
		c.Database.Path = "data/dnsmon.db"
	}
	if c.Monitor.IntervalSeconds <= 0 {
		c.Monitor.IntervalSeconds = defaultIntervalSeconds
	}
	if c.Monitor.TimeoutSeconds <= 0 {
		c.Monitor.TimeoutSeconds = defaultTimeoutSeconds
	}
	if c.Telegram.PollTimeoutSeconds <= 0 {
		c.Telegram.PollTimeoutSeconds = defaultPollTimeout
	}
	if c.Pairing.TokenTTLMinutes <= 0 {
		c.Pairing.TokenTTLMinutes = defaultTokenTTLMinutes
	}
	if c.Daemon.PIDFile == "" {
		c.Daemon.PIDFile = filepath.Join(filepath.Dir(c.Database.Path), "dnsmon.pid")
	}
	if c.Daemon.LogFile == "" {
		c.Daemon.LogFile = filepath.Join(filepath.Dir(c.Database.Path), "dnsmon.log")
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "text"
	}
}

// Validate reports the first rule violation found in the configuration.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Database.Path) == "" {
		return fmt.Errorf("%w: database.path must not be empty", shared.ErrInvalid)
	}
	if strings.TrimSpace(c.Telegram.Token) == "" {
		return fmt.Errorf("%w: telegram.token must not be empty (or set %s)", shared.ErrInvalid, EnvToken)
	}
	for _, username := range c.Telegram.NotifyUsernames {
		if strings.TrimSpace(username) == "" {
			return fmt.Errorf("%w: telegram.notify_usernames contains an empty entry", shared.ErrInvalid)
		}
	}
	for _, resolver := range c.Monitor.Resolvers {
		if strings.TrimSpace(resolver) == "" {
			return fmt.Errorf("%w: monitor.resolvers contains an empty entry", shared.ErrInvalid)
		}
	}
	for index, rec := range c.Monitor.Records {
		if _, err := record.CanonicalName(rec.Name); err != nil {
			return fmt.Errorf("monitor.records[%d]: %w", index, err)
		}
		if _, err := record.ParseType(rec.Type); err != nil {
			return fmt.Errorf("monitor.records[%d]: %w", index, err)
		}
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("%w: log.level %q is not a known level", shared.ErrInvalid, c.Log.Level)
	}
	switch strings.ToLower(c.Log.Format) {
	case "text", "json":
	default:
		return fmt.Errorf("%w: log.format %q is not a known format", shared.ErrInvalid, c.Log.Format)
	}
	return nil
}

// Interval returns the delay between two consecutive checks.
func (c *Config) Interval() time.Duration {
	return time.Duration(c.Monitor.IntervalSeconds) * time.Second
}

// QueryTimeout returns the timeout of a single DNS query.
func (c *Config) QueryTimeout() time.Duration {
	return time.Duration(c.Monitor.TimeoutSeconds) * time.Second
}

// PollTimeout returns how long a Telegram long poll waits for updates.
func (c *Config) PollTimeout() time.Duration {
	return time.Duration(c.Telegram.PollTimeoutSeconds) * time.Second
}

// TokenTTL returns the lifetime of a pairing token.
func (c *Config) TokenTTL() time.Duration {
	return time.Duration(c.Pairing.TokenTTLMinutes) * time.Minute
}

// Usernames returns the canonical, de-duplicated list of accounts allowed to
// pair with the bot.
func (c *Config) Usernames() []string {
	seen := make(map[string]struct{}, len(c.Telegram.NotifyUsernames))
	result := make([]string, 0, len(c.Telegram.NotifyUsernames))
	for _, raw := range c.Telegram.NotifyUsernames {
		canonical, err := canonicalUsername(raw)
		if err != nil {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	return result
}

// RecordEnabled reports whether a declared record is active.
func (r Record) RecordEnabled() bool {
	return r.Enabled == nil || *r.Enabled
}

func canonicalUsername(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "@")
	trimmed = strings.ToLower(trimmed)
	if trimmed == "" {
		return "", fmt.Errorf("%w: empty username", shared.ErrInvalid)
	}
	return trimmed, nil
}
