// Package app is the composition root of the monitor. It wires the domain,
// the application services, the infrastructure adapters and the agents into
// a single runnable application.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/agent"
	"github.com/aognio/dns-change-monitor/pkg/agent/notifier"
	"github.com/aognio/dns-change-monitor/pkg/agent/pairer"
	"github.com/aognio/dns-change-monitor/pkg/agent/poller"
	appnotification "github.com/aognio/dns-change-monitor/pkg/app/notification"
	apppairing "github.com/aognio/dns-change-monitor/pkg/app/pairing"
	"github.com/aognio/dns-change-monitor/pkg/app/polling"
	"github.com/aognio/dns-change-monitor/pkg/config"
	"github.com/aognio/dns-change-monitor/pkg/domain/change"
	"github.com/aognio/dns-change-monitor/pkg/domain/notification"
	"github.com/aognio/dns-change-monitor/pkg/domain/record"
	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
	"github.com/aognio/dns-change-monitor/pkg/domain/subscriber"
	domaintelegram "github.com/aognio/dns-change-monitor/pkg/domain/telegram"
	"github.com/aognio/dns-change-monitor/pkg/infra/bus"
	"github.com/aognio/dns-change-monitor/pkg/infra/dns"
	"github.com/aognio/dns-change-monitor/pkg/infra/persistence"
	infratelelegram "github.com/aognio/dns-change-monitor/pkg/infra/telegram"
	"gorm.io/gorm"
)

// App is the assembled application.
type App struct {
	Config *config.Config
	Logger *slog.Logger

	db         *gorm.DB
	repos      persistence.Repositories
	bus        *bus.MemoryBus
	clock      shared.Clock
	ids        shared.IDGenerator
	resolver   record.Resolver
	gateway    domaintelegram.Gateway
	notifier   notification.Notifier
	supervisor *agent.Supervisor
}

// Open opens the database and prepares the repositories. It is the entry
// point for the command line utilities that do not need the Telegram bot.
func Open(cfg *config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := persistence.Open(cfg.Database.Path, false)
	if err != nil {
		return nil, err
	}

	return &App{
		Config: cfg,
		Logger: logger,
		db:     db,
		repos:  persistence.NewRepositories(db),
		bus:    bus.New(logger),
		clock:  shared.SystemClock{},
		ids:    shared.UUIDGenerator{},
	}, nil
}

// Repositories gives access to the stored state.
func (a *App) Repositories() persistence.Repositories { return a.repos }

// Bus gives access to the event bus.
func (a *App) Bus() shared.EventBus { return a.bus }

// Clock gives access to the clock of the application.
func (a *App) Clock() shared.Clock { return a.clock }

// IDs gives access to the identifier generator of the application.
func (a *App) IDs() shared.IDGenerator { return a.ids }

// ConnectTelegram creates the Telegram gateway and the notifier bound to it.
func (a *App) ConnectTelegram() error {
	gateway, err := infratelelegram.New(infratelelegram.Options{
		Token:       a.Config.Telegram.Token,
		PollTimeout: a.Config.PollTimeout(),
		Logger:      a.Logger,
	})
	if err != nil {
		return err
	}
	a.gateway = gateway
	a.notifier = infratelelegram.NewNotifier(gateway, a.Logger)
	return nil
}

// SetResolver replaces the DNS resolver used by the polling service. It
// exists so that the monitor can be driven by another source of answers.
func (a *App) SetResolver(resolver record.Resolver) { a.resolver = resolver }

// SetGateway replaces the Telegram gateway and the notifier used by the
// agents. The pairing agent is only assembled when a gateway is present.
func (a *App) SetGateway(gateway domaintelegram.Gateway, notifier notification.Notifier) {
	a.gateway = gateway
	a.notifier = notifier
}

// Services builds the application services.
func (a *App) Services() (*polling.Service, *appnotification.Service, *apppairing.Service) {
	resolver := a.resolver
	if resolver == nil {
		resolver = dns.New(dns.Options{
			Servers: a.Config.Monitor.Resolvers,
			Timeout: a.Config.QueryTimeout(),
		})
	}

	pollingService := polling.New(
		a.repos.Targets,
		a.repos.Snapshots,
		resolver,
		a.repos.Changes,
		a.bus,
		a.ids,
		a.clock,
		a.Logger,
		polling.Options{NotifyBaseline: a.Config.Monitor.NotifyOnBaseline},
	)

	notificationService := appnotification.New(
		a.repos.Subscribers,
		a.repos.Changes,
		a.notifier,
		a.clock,
		a.Logger,
	)

	pairingService := apppairing.New(
		a.repos.Tokens,
		a.repos.Subscribers,
		a.Config.Usernames(),
		a.ids,
		a.clock,
		a.Logger,
	)

	return pollingService, notificationService, pairingService
}

// Assemble creates the agents and the supervisor that runs them.
func (a *App) Assemble() error {
	if a.gateway == nil {
		if err := a.ConnectTelegram(); err != nil {
			return err
		}
	}

	pollingService, notificationService, pairingService := a.Services()

	agents := []agent.Agent{
		poller.New(poller.Options{
			Service:  pollingService,
			Interval: a.Config.Interval(),
			Logger:   a.Logger,
		}),
		notifier.New(notifier.Options{
			Service: notificationService,
			Bus:     a.bus,
			Logger:  a.Logger,
		}),
	}
	if a.gateway != nil {
		agents = append(agents, pairer.New(pairer.Options{
			Gateway:  a.gateway,
			Service:  pairingService,
			Notifier: a.notifier,
			Status:   a,
			Logger:   a.Logger,
		}))
	}

	a.supervisor = agent.NewSupervisor(agent.Options{
		Agents:  agents,
		Logger:  a.Logger,
		Backoff: 5 * time.Second,
	})
	return nil
}

// Seed brings the stored state in line with the configuration file: the
// declared records become monitored targets and the declared usernames become
// subscribers waiting to be paired.
func (a *App) Seed(ctx context.Context) error {
	now := a.clock.Now()

	existing, err := a.repos.Targets.List(ctx)
	if err != nil {
		return err
	}

	declared := make(map[string]struct{})
	for _, declaredRecord := range a.Config.Monitor.Records {
		name, err := record.CanonicalName(declaredRecord.Name)
		if err != nil {
			return err
		}
		recordType, err := record.ParseType(declaredRecord.Type)
		if err != nil {
			return err
		}
		resolver := declaredRecord.Resolver
		if resolver == "" && len(a.Config.Monitor.Resolvers) > 0 {
			resolver = a.Config.Monitor.Resolvers[0]
		}

		target, err := record.NewTarget(name, recordType, resolver, record.SourceConfig, now)
		if err != nil {
			return err
		}
		target.ID = a.ids.NewID()
		target.Enabled = declaredRecord.RecordEnabled()

		if matched, ok := findTarget(existing, target.Identity()); ok {
			target.ID = matched.ID
			target.CreatedAt = matched.CreatedAt
		}

		if err := a.repos.Targets.Upsert(ctx, target); err != nil {
			return err
		}
		declared[target.Identity()] = struct{}{}
	}

	if err := a.removeUndeclared(ctx, declared); err != nil {
		return err
	}
	return a.seedSubscribers(ctx)
}

// removeUndeclared deletes the targets that came from the configuration file
// and are no longer declared in it.
func (a *App) removeUndeclared(ctx context.Context, declared map[string]struct{}) error {
	existing, err := a.repos.Targets.List(ctx)
	if err != nil {
		return err
	}
	for _, target := range existing {
		if target.Source != record.SourceConfig {
			continue
		}
		if _, ok := declared[target.Identity()]; ok {
			continue
		}
		if err := a.repos.Targets.Delete(ctx, target.ID); err != nil {
			return err
		}
		a.Logger.Info("target removed", slog.String("target", target.Name))
	}
	return nil
}

// seedSubscribers registers the usernames allowed to use the bot.
func (a *App) seedSubscribers(ctx context.Context) error {
	now := a.clock.Now()
	for _, username := range a.Config.Usernames() {
		if _, err := a.repos.Subscribers.FindByUsername(ctx, username); err == nil {
			// The account is already known. A disabled account stays
			// disabled until it is removed.
			continue
		}
		entry, err := subscriber.New(a.ids.NewID(), username, now)
		if err != nil {
			return err
		}
		if err := a.repos.Subscribers.Upsert(ctx, entry); err != nil {
			return err
		}
		a.Logger.Info("subscriber registered", slog.String("username", username))
	}
	return nil
}

// Run starts the agents and blocks until the context is cancelled.
func (a *App) Run(ctx context.Context) error {
	if a.supervisor == nil {
		if err := a.Assemble(); err != nil {
			return err
		}
	}
	a.Logger.Info("monitor started",
		slog.String("database", a.Config.Database.Path),
		slog.Duration("interval", a.Config.Interval()),
		slog.Int("agents", len(a.supervisor.Statuses())))
	return a.supervisor.Run(ctx)
}

// Close releases the resources held by the application.
func (a *App) Close() error {
	if gateway, ok := a.gateway.(*infratelelegram.Gateway); ok && gateway != nil {
		gateway.Stop()
	}
	if a.db == nil {
		return nil
	}
	sqlDB, err := a.db.DB()
	if err != nil {
		return fmt.Errorf("access database handle: %w", err)
	}
	return sqlDB.Close()
}

// CheckState describes what the answer of a record looks like compared to
// the last observation.
type CheckState string

// The possible states of a checked record.
const (
	// CheckUnchanged means the answer is the one of the last observation.
	CheckUnchanged CheckState = "unchanged"
	// CheckNew means the record was never observed before.
	CheckNew CheckState = "new"
	// CheckChanged means the answer differs from the last observation.
	CheckChanged CheckState = "changed"
	// CheckMissing means the record could not be resolved.
	CheckMissing CheckState = "missing"
)

// CheckResult is the outcome of resolving a single record.
type CheckResult struct {
	Target record.Target
	State  CheckState
	Values []string
	Error  string
}

// Check resolves every monitored record once and compares the answers with
// the last observation. It records nothing, so it can be used to check the
// setup before the monitor is started.
func (a *App) Check(ctx context.Context) ([]CheckResult, error) {
	targets, err := a.repos.Targets.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}

	resolver := a.resolver
	if resolver == nil {
		resolver = dns.New(dns.Options{
			Servers: a.Config.Monitor.Resolvers,
			Timeout: a.Config.QueryTimeout(),
		})
	}

	results := make([]CheckResult, 0, len(targets))
	for _, target := range targets {
		if !target.Enabled {
			continue
		}
		result := CheckResult{Target: target, State: CheckUnchanged}

		resolution, err := resolver.Resolve(ctx, target)
		if err != nil {
			if !errors.Is(err, record.ErrNoRecords) {
				result.State = CheckMissing
				result.Error = err.Error()
				results = append(results, result)
				continue
			}
			// The name carries no record yet. It is compared with the
			// previous observation like any other answer.
			resolution = record.Resolution{}
		}
		result.Values = resolution.Values

		previous, err := a.repos.Snapshots.Latest(ctx, target.ID)
		switch {
		case err == nil:
			added, removed := record.DiffValues(previous.Values, resolution.Values)
			if len(added) > 0 || len(removed) > 0 {
				result.State = CheckChanged
			}
		case isNotFoundError(err):
			result.State = CheckNew
		default:
			return results, fmt.Errorf("load snapshot of %s: %w", target.Name, err)
		}
		results = append(results, result)
	}
	return results, nil
}

// isNotFoundError reports whether an error means that nothing was stored yet.
func isNotFoundError(err error) bool {
	return err != nil && strings.Contains(err.Error(), shared.ErrNotFound.Error())
}

// Status renders the state of the monitor for the status command.
func (a *App) Status(ctx context.Context) (string, error) {
	targets, err := a.repos.Targets.List(ctx)
	if err != nil {
		return "", err
	}
	enabled := 0
	for _, target := range targets {
		if target.Enabled {
			enabled++
		}
	}

	subscribers, err := a.repos.Subscribers.List(ctx)
	if err != nil {
		return "", err
	}
	paired := 0
	for _, entry := range subscribers {
		if entry.IsPaired() {
			paired++
		}
	}

	recent, err := a.repos.Changes.List(ctx, 5)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString("DNS change monitor\n")
	builder.WriteString(fmt.Sprintf("Monitored records : %d (%d enabled)\n", len(targets), enabled))
	builder.WriteString(fmt.Sprintf("Paired accounts   : %d of %d\n", paired, len(subscribers)))
	builder.WriteString(fmt.Sprintf("Check interval    : %s\n", a.Config.Interval()))
	if len(recent) == 0 {
		builder.WriteString("Recent changes    : none\n")
		return builder.String(), nil
	}
	builder.WriteString("Recent changes    :\n")
	for _, detected := range recent {
		builder.WriteString(fmt.Sprintf("  %s %s %s\n",
			detected.DetectedAt.UTC().Format(time.RFC3339),
			detected.TargetName,
			detected.Summary()))
	}
	return builder.String(), nil
}

// findTarget looks a target up by its natural identity.
func findTarget(targets []record.Target, identity string) (record.Target, bool) {
	for _, target := range targets {
		if target.Identity() == identity {
			return target, true
		}
	}
	return record.Target{}, false
}

// Compile time check that the application satisfies the status port.
var _ pairer.StatusProvider = (*App)(nil)

// Compile time check that the change repository is reachable.
var _ change.Repository = (*persistence.ChangeRepository)(nil)
