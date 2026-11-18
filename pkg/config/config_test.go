package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aognio/dns-change-monitor/pkg/config"
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
	if cfg.Log.File == "" {
		t.Fatal("the defaults must fill the path of the plain text log file")
	}
	if cfg.Log.FileLevel != "info" {
		t.Fatalf("unexpected log file level: %q", cfg.Log.FileLevel)
	}
}

func TestLoadRejectsAnUnknownLogFileLevel(t *testing.T) {
	path := writeConfig(t, `
[telegram]
token = "123456:secret"

[log]
file_level = "chatty"
`)

	if _, err := config.Load(path); err == nil {
		t.Fatal("expected an error for an unknown log file level")
	}
}

func TestLoadAcceptsADisabledLogFile(t *testing.T) {
	path := writeConfig(t, `
[telegram]
token = "123456:secret"

[log]
file = "none"
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Log.File != "" {
		t.Fatalf("unexpected log file: %q", cfg.Log.File)
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

func TestLoadReadsTheCheckDeclaration(t *testing.T) {
	path := writeConfig(t, `
[telegram]
token = "123456:secret"

[[monitor.records]]
name = "example.com"
type = "A"
mode = "exact"
expect = ["1.2.3.4", "5.6.7.8"]

[[monitor.records]]
name = "example.org"
type = "TXT"
mode = "regex"
expect = ["^v=spf1"]

[[monitor.records]]
name = "example.net"
type = "NS"
mode = "any"
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Monitor.Records) != 3 {
		t.Fatalf("unexpected records: %+v", cfg.Monitor.Records)
	}
	first := cfg.Monitor.Records[0]
	if first.Mode != "exact" {
		t.Fatalf("unexpected mode: %q", first.Mode)
	}
	if len(first.Expect) != 2 || first.Expect[0] != "1.2.3.4" {
		t.Fatalf("unexpected expectation: %v", first.Expect)
	}
	if cfg.Monitor.Records[1].Mode != "regex" {
		t.Fatalf("unexpected mode: %q", cfg.Monitor.Records[1].Mode)
	}
	if cfg.Monitor.Records[2].Mode != "any" {
		t.Fatalf("unexpected mode: %q", cfg.Monitor.Records[2].Mode)
	}
}

func TestLoadRejectsTheInvalidCheckDeclarations(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"expect without a mode", `
mode = ""
expect = ["1.2.3.4"]`},
		{"expect with the change mode", `
mode = "change"
expect = ["1.2.3.4"]`},
		{"an unknown mode", `
mode = "sometimes"`},
		{"exact without an expectation", `
mode = "exact"`},
		{"regex without an expectation", `
mode = "regex"`},
		{"regex with two expectations", `
mode = "regex"
expect = ["a", "b"]`},
		{"regex with an invalid pattern", `
mode = "regex"
expect = ["([a-z"]`},
		{"any with an expectation", `
mode = "any"
expect = ["1.2.3.4"]`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeConfig(t, `
[telegram]
token = "123456:secret"

[[monitor.records]]
name = "example.com"
type = "A"`+testCase.body+`
`)
			if _, err := config.Load(path); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoadNamesTheOffendingRecord(t *testing.T) {
	path := writeConfig(t, `
[telegram]
token = "123456:secret"

[[monitor.records]]
name = "example.com"
type = "A"

[[monitor.records]]
name = "example.org"
type = "A"
mode = "exact"
`)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "monitor.records[1]") {
		t.Fatalf("the error must name the offending record: %v", err)
	}
}

func TestLoadRejectsAMissingFile(t *testing.T) {
	if _, err := config.Load(filepath.Join(t.TempDir(), "absent.toml")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
