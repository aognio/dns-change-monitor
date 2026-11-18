// Package daemon turns the monitor into a background process and keeps track
// of it through a process identifier file.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// EnvDaemonized marks a process that was started in the background. The
// child reads it to know that it must not fork a second time.
const EnvDaemonized = "DNSMON_DAEMONIZED"

// IsDaemonized reports whether the current process runs as a background
// daemon.
func IsDaemonized() bool {
	return strings.EqualFold(os.Getenv(EnvDaemonized), "1")
}

// WritePIDFile records the identifier of the running process.
func WritePIDFile(path string) error {
	if path == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create pid file directory %s: %w", dir, err)
		}
	}
	payload := fmt.Sprintf("%d\n", os.Getpid())
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		return fmt.Errorf("write pid file %s: %w", path, err)
	}
	return nil
}

// RemovePIDFile deletes the process identifier file.
func RemovePIDFile(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

// ReadPIDFile returns the identifier stored in the process identifier file.
func ReadPIDFile(path string) (int, error) {
	if path == "" {
		return 0, fmt.Errorf("no pid file configured")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read pid file %s: %w", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("parse pid file %s: %w", path, err)
	}
	return pid, nil
}

// waitForReady polls the process identifier file until the child reports
// that it started successfully.
func waitForReady(pidFile string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := ReadPIDFile(pidFile); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("the daemon did not start within %s", timeout)
}

// tailOf returns the last lines of a file, so that the failure of a daemon
// can be reported with the reason it wrote down.
func tailOf(path string) string {
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if len(kept) > 0 && kept[len(kept)-1] == trimmed {
			continue
		}
		kept = append(kept, trimmed)
	}
	if len(kept) == 0 {
		return ""
	}
	return "\n" + strings.Join(kept, "\n")
}
