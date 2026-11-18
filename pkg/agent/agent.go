// Package agent holds the runtime that keeps the autonomous parts of the
// monitor alive. Every agent owns one goal and reports its health to the
// supervisor.
package agent

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Agent is an autonomous unit of work with a single goal.
type Agent interface {
	// Name identifies the agent in the logs.
	Name() string
	// Goal states what the agent is trying to achieve.
	Goal() string
	// Run performs the work until the context is cancelled.
	Run(ctx context.Context) error
}

// State describes the life cycle of an agent.
type State string

// The states an agent can be in.
const (
	// StateStarting means the agent has not run yet.
	StateStarting State = "starting"
	// StateRunning means the agent is working.
	StateRunning State = "running"
	// StateStopped means the agent finished or was cancelled.
	StateStopped State = "stopped"
	// StateFailed means the agent stopped on an error.
	StateFailed State = "failed"
)

// Status is the health of one agent.
type Status struct {
	Name      string    `json:"name"`
	Goal      string    `json:"goal"`
	State     State     `json:"state"`
	Restarts  int       `json:"restarts"`
	LastError string    `json:"last_error,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Supervisor runs a set of agents and restarts the ones that fail.
type Supervisor struct {
	agents  []Agent
	logger  *slog.Logger
	backoff time.Duration
	mutex   sync.RWMutex
	status  map[string]Status
}

// Options configure the supervisor.
type Options struct {
	// Agents are the units of work to supervise.
	Agents []Agent
	// Logger receives the diagnostics of the supervisor.
	Logger *slog.Logger
	// Backoff is the delay before a failed agent is restarted.
	Backoff time.Duration
}

// NewSupervisor builds a supervisor.
func NewSupervisor(options Options) *Supervisor {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	backoff := options.Backoff
	if backoff <= 0 {
		backoff = 5 * time.Second
	}
	supervisor := &Supervisor{
		agents:  options.Agents,
		logger:  logger,
		backoff: backoff,
		status:  make(map[string]Status, len(options.Agents)),
	}
	for _, current := range options.Agents {
		supervisor.status[current.Name()] = Status{
			Name:  current.Name(),
			Goal:  current.Goal(),
			State: StateStarting,
		}
	}
	return supervisor
}

// Run starts every agent and waits for the context to be cancelled. A failed
// agent is restarted after the backoff delay.
func (s *Supervisor) Run(ctx context.Context) error {
	var waitGroup sync.WaitGroup
	for _, current := range s.agents {
		waitGroup.Add(1)
		go func(agent Agent) {
			defer waitGroup.Done()
			s.supervise(ctx, agent)
		}(current)
	}
	<-ctx.Done()
	waitGroup.Wait()
	return ctx.Err()
}

// supervise runs one agent, restarting it when it fails.
func (s *Supervisor) supervise(ctx context.Context, current Agent) {
	for {
		if ctx.Err() != nil {
			s.record(current, func(status *Status) {
				status.State = StateStopped
				status.UpdatedAt = time.Now()
			})
			return
		}

		s.record(current, func(status *Status) {
			status.State = StateRunning
			status.StartedAt = time.Now()
			status.UpdatedAt = time.Now()
			status.LastError = ""
		})
		s.logger.Info("agent started",
			slog.String("agent", current.Name()),
			slog.String("goal", current.Goal()))

		err := current.Run(ctx)

		switch {
		case err == nil, errors.Is(err, context.Canceled), ctx.Err() != nil:
			s.record(current, func(status *Status) {
				status.State = StateStopped
				status.UpdatedAt = time.Now()
			})
			s.logger.Info("agent stopped", slog.String("agent", current.Name()))
			return
		default:
			s.record(current, func(status *Status) {
				status.State = StateFailed
				status.Restarts++
				status.LastError = err.Error()
				status.UpdatedAt = time.Now()
			})
			s.logger.Error("agent failed, restarting",
				slog.String("agent", current.Name()),
				slog.String("error", err.Error()),
				slog.Duration("backoff", s.backoff))

			select {
			case <-ctx.Done():
				return
			case <-time.After(s.backoff):
			}
		}
	}
}

// record updates the status of an agent.
func (s *Supervisor) record(current Agent, mutate func(*Status)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	status, ok := s.status[current.Name()]
	if !ok {
		status = Status{Name: current.Name(), Goal: current.Goal()}
	}
	mutate(&status)
	s.status[current.Name()] = status
}

// Statuses returns the health of every supervised agent.
func (s *Supervisor) Statuses() []Status {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	statuses := make([]Status, 0, len(s.status))
	for _, current := range s.agents {
		if status, ok := s.status[current.Name()]; ok {
			statuses = append(statuses, status)
		}
	}
	return statuses
}
