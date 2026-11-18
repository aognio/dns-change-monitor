//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Spawn starts a detached copy of the binary and returns once the child has
// written its process identifier file.
func Spawn(executable string, args []string, logFile string, pidFile string) (int, error) {
	if logFile != "" {
		if dir := dirOf(logFile); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return 0, fmt.Errorf("create log directory %s: %w", dir, err)
			}
		}
	}

	output, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open log file %s: %w", logFile, err)
	}
	defer output.Close()

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer devNull.Close()

	command := exec.Command(executable, args...)
	command.Env = append(os.Environ(), EnvDaemonized+"=1")
	command.Stdin = devNull
	command.Stdout = output
	command.Stderr = output
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

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
			return 0, fmt.Errorf("the daemon exited during startup: %w%s", waitErr, tailOf(logFile))
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
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal process %d: %w", pid, err)
	}
	return nil
}

// IsRunning reports whether a process identifier refers to a live process.
func IsRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func dirOf(path string) string {
	for index := len(path) - 1; index >= 0; index-- {
		if path[index] == '/' {
			return path[:index]
		}
	}
	return ""
}
