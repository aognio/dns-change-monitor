package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gnrfan/dns-change-monitor/pkg/config"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return path
}

func TestLoadReadsTheWholeConfiguration(t *testing.T) {
	path := writeConfig(t, `
[database]
path = "/tmp/dnsmon.db"

[telegram]
token = "123456:secret"
notify_usernames = ["@Alice", "bob"]

[monitor]
interval_seconds = 30
resolvers = ["1.1.1.1:53"]

[[monitor.records]]
name = "example.com"
type = "a"
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database.Path != "/tmp/dnsmon.db" {
		t.Fatalf("unexpected database path: %q", cfg.Database.Path)
	}
	if cfg.Monitor.IntervalSeconds != 30 {
		t.Fatalf("unexpected interval: %d", cfg.Monitor.IntervalSeconds)
	}
	if len(cfg.Monitor.Records) != 1 || cfg.Monitor.Records[0].Name != "example.com" {
		t.Fatalf("unexpected records: %+v", cfg.Monitor.Records)
	}
	usernames := cfg.Usernames()
	if len(usernames) != 2 || usernames[0] != "alice" || usernames[1] != "bob" {
		t.Fatalf("unexpected usernames: %v", usernames)
	}
}

func TestLoadAppliesTheDefaults(t *testing.T) {
	path := writeConfig(t, `
[telegram]
token = "123456:secret"
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Monitor.IntervalSeconds != 60 {
		t.Fatalf("unexpected interval: %d", cfg.Monitor.IntervalSeconds)
	}
	if cfg.Database.Path == "" || cfg.Daemon.PIDFile == "" || cfg.Daemon.LogFile == "" {
		t.Fatal("the defaults must fill the database and daemon paths")
	}
	if cfg.Pairing.TokenTTLMinutes != 30 {
		t.Fatalf("unexpected token lifetime: %d", cfg.Pairing.TokenTTLMinutes)
	}
}

func TestLoadRejectsAMissingToken(t *testing.T) {
	path := writeConfig(t, `
[database]
path = "/tmp/dnsmon.db"
`)

	if _, err := config.Load(path); err == nil {
		t.Fatal("expected an error for a missing token")
	}
}

func TestLoadRejectsAnUnknownRecordType(t *testing.T) {
	path := writeConfig(t, `
[telegram]
token = "123456:secret"

[[monitor.records]]
name = "example.com"
type = "WAT"
`)

	if _, err := config.Load(path); err == nil {
		t.Fatal("expected an error for an unknown record type")
	}
}

func TestLoadRejectsAMissingFile(t *testing.T) {
	if _, err := config.Load(filepath.Join(t.TempDir(), "absent.toml")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
