package daemon_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gnrfan/dns-change-monitor/pkg/infra/daemon"
)

const pidFileVariable = "DNSMON_TEST_PID_FILE"

// TestMain turns the test binary into the daemon it spawns.
func TestMain(m *testing.M) {
	if daemon.IsDaemonized() {
		pidFile := os.Getenv(pidFileVariable)
		if err := daemon.WritePIDFile(pidFile); err != nil {
			os.Exit(1)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSpawnStartsADetachedProcess(t *testing.T) {
	directory := t.TempDir()
	pidFile := filepath.Join(directory, "daemon.pid")
	logFile := filepath.Join(directory, "daemon.log")

	t.Setenv(pidFileVariable, pidFile)

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pid, err := daemon.Spawn(executable, nil, logFile, pidFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("unexpected process identifier: %d", pid)
	}

	stored, err := daemon.ReadPIDFile(pidFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored != pid {
		t.Fatalf("the pid file holds %d, the daemon runs with %d", stored, pid)
	}
	if !daemon.IsRunning(stored) {
		t.Fatal("the daemon must be running")
	}

	if err := daemon.Stop(stored); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !daemon.IsRunning(stored) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the daemon must stop")
}

func TestReadPIDFileReportsAMissingFile(t *testing.T) {
	if _, err := daemon.ReadPIDFile(filepath.Join(t.TempDir(), "absent.pid")); err == nil {
		t.Fatal("expected an error for a missing pid file")
	}
}

func TestWriteAndRemovePIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "daemon.pid")
	if err := daemon.WritePIDFile(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("the pid file holds %d, the process runs with %d", pid, os.Getpid())
	}

	daemon.RemovePIDFile(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the pid file must be removed")
	}
}
