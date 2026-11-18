//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Spawn starts a detached copy of the binary and returns once the child has
// written its process identifier file.
func Spawn(executable string, args []string, logFile string, pidFile string) (int, error) {
	output, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open log file %s: %w", logFile, err)
	}
	defer output.Close()

	command := exec.Command(executable, args...)
	command.Env = append(os.Environ(), EnvDaemonized+"=1")
	command.Stdout = output
	command.Stderr = output

	if err := command.Start(); err != nil {
		return 0, fmt.Errorf("start daemon: %w", err)
	}
	pid := command.Process.Pid

	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()

	deadline := time.After(20 * time.Second)
	for {
		select {
		case waitErr := <-exited:
			return 0, fmt.Errorf("the daemon exited during startup: %w", waitErr)
		case <-deadline:
			return 0, fmt.Errorf("the daemon did not start within 20s")
		case <-time.After(50 * time.Millisecond):
			if _, err := ReadPIDFile(pidFile); err == nil {
				return pid, nil
			}
		}
	}
}

// Stop asks a running daemon to terminate.
func Stop(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	return process.Kill()
}

// IsRunning reports whether a process identifier refers to a live process.
func IsRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = process.Release()
	return true
}
