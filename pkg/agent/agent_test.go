package agent_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/agent"
)

// failingAgent fails a number of times before it stops.
type failingAgent struct {
	name     string
	failures int32
	runs     int32
}

func (a *failingAgent) Name() string { return a.name }

func (a *failingAgent) Goal() string { return "fail on purpose" }

func (a *failingAgent) Run(context.Context) error {
	atomic.AddInt32(&a.runs, 1)
	if atomic.LoadInt32(&a.failures) > 0 {
		atomic.AddInt32(&a.failures, -1)
		return errors.New("the agent failed on purpose")
	}
	return nil
}

func TestSupervisorRestartsAFailingAgent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	worker := &failingAgent{name: "worker", failures: 2}
	supervisor := agent.NewSupervisor(agent.Options{
		Agents:  []agent.Agent{worker},
		Logger:  logger,
		Backoff: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = supervisor.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&worker.runs) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	if atomic.LoadInt32(&worker.runs) < 3 {
		t.Fatalf("the agent must have been restarted, it ran %d times", atomic.LoadInt32(&worker.runs))
	}
}

func TestSupervisorReportsTheHealthOfTheAgents(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	worker := &failingAgent{name: "worker"}
	supervisor := agent.NewSupervisor(agent.Options{
		Agents: []agent.Agent{worker},
		Logger: logger,
	})

	statuses := supervisor.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("expected one agent, got %d", len(statuses))
	}
	if statuses[0].Name != "worker" {
		t.Fatalf("unexpected agent: %q", statuses[0].Name)
	}
	if statuses[0].Goal == "" {
		t.Fatal("the goal of the agent must be reported")
	}
}

func TestSupervisorStopsWhenTheContextIsCancelled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	supervisor := agent.NewSupervisor(agent.Options{
		Agents: []agent.Agent{&failingAgent{name: "worker"}},
		Logger: logger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the supervisor must stop when the context is cancelled")
	}
}
