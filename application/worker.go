// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package application

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/infrastructure/logging"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
)

// SubscriberGroup defines one named background subscriber: the handler that
// processes the event feed and any per-group option overrides. Grouping decides
// what shares a checkpoint and fails, lags, and parks together — peripheral
// subsystems (knowledge-graph sync, denormalization) each get their own group
// so an outage in one never stalls the others.
//
// Groups are contributed into the Fx graph via the "subscriber_groups" value
// group (see AsSubscriberGroup); the Manager collects them all and builds a
// pericarp Subscriber per group.
type SubscriberGroup struct {
	// Name is the subscriber's checkpoint name. Processes using the same name
	// share one position, so it must be stable across restarts and unique
	// across groups.
	Name string
	// Handler processes a single event from the feed. A group-private
	// EventDispatcher's Dispatch method satisfies this signature.
	Handler subscriptions.Handler
	// Options are per-group overrides appended after the Manager's defaults
	// (e.g. a smaller batch size for an expensive handler).
	Options []subscriptions.SubscriberOption
}

// WakeSource hands out wake channels so idle subscribers react to new commits
// instead of waiting out the poll interval. Each subscriber gets its own
// channel (channel receives are point-to-point). Implemented by pericarp's
// InProcessNotifier (SQLite / single process) and PostgresListener (cross
// process via LISTEN/NOTIFY).
type WakeSource interface {
	Subscribe() <-chan struct{}
}

// Manager owns the background subscriber runtime: it builds a pericarp
// Subscriber per registered group, runs them as crash-safe workers, drains
// them on shutdown, and periodically reports checkpoint lag. The same Manager
// also backs the operator CLI (list/replay parked events, reset checkpoints),
// which constructs subscribers on demand without running them.
type Manager struct {
	eventStore  domain.EventStore
	checkpoints subscriptions.CheckpointStore
	parking     subscriptions.ParkingLot
	notifier    WakeSource // in-process notifier; used for the SQLite wake path
	cfg         config.WorkerConfig
	postgres    bool
	dsn         string
	logger      entities.Logger
	slog        *slog.Logger

	groups []SubscriberGroup
	byName map[string]SubscriberGroup

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// ManagerParams bundles the Manager's dependencies. groups arrives via the
// "subscriber_groups" Fx value group, so any provider can contribute one.
type ManagerParams struct {
	EventStore  domain.EventStore
	Checkpoints subscriptions.CheckpointStore
	Parking     subscriptions.ParkingLot
	Notifier    WakeSource
	Config      config.Config
	Logger      entities.Logger
	Groups      []SubscriberGroup
}

// NewManager validates the group set and constructs the runtime. It does not
// start any processing; call Start (or use a single subscriber via the CLI
// accessors).
func NewManager(p ManagerParams) (*Manager, error) {
	byName := make(map[string]SubscriberGroup, len(p.Groups))
	for _, g := range p.Groups {
		if g.Name == "" {
			return nil, fmt.Errorf("subscriber group has empty name")
		}
		if g.Handler == nil {
			return nil, fmt.Errorf("subscriber group %q has nil handler", g.Name)
		}
		if _, dup := byName[g.Name]; dup {
			return nil, fmt.Errorf("duplicate subscriber group name %q", g.Name)
		}
		byName[g.Name] = g
	}
	logger := p.Logger
	if logger == nil {
		logger = noopWorkerLogger{}
	}
	return &Manager{
		eventStore:  p.EventStore,
		checkpoints: p.Checkpoints,
		parking:     p.Parking,
		notifier:    p.Notifier,
		cfg:         p.Config.Worker,
		postgres:    p.Config.IsPostgres(),
		dsn:         p.Config.DatabaseDSN,
		logger:      logger,
		slog:        subscriberSlog(logger),
		groups:      p.Groups,
		byName:      byName,
	}, nil
}

// noopWorkerLogger is the fallback when no entities.Logger is wired (only in tests —
// the Fx graph always provides one). It keeps the Manager from dereferencing a
// nil logger.
type noopWorkerLogger struct{}

func (noopWorkerLogger) Debug(context.Context, string, ...any) {}
func (noopWorkerLogger) Info(context.Context, string, ...any)  {}
func (noopWorkerLogger) Warn(context.Context, string, ...any)  {}
func (noopWorkerLogger) Error(context.Context, string, ...any) {}

// baseOptions returns the per-group defaults applied before each group's own
// overrides: parking, the bridged logger, and the configured
// batch/poll/retry/backoff knobs.
func (m *Manager) baseOptions() []subscriptions.SubscriberOption {
	return []subscriptions.SubscriberOption{
		subscriptions.WithParkingLot(m.parking),
		subscriptions.WithLogger(m.slog),
		subscriptions.WithBatchSize(m.cfg.BatchSize),
		subscriptions.WithPollInterval(m.cfg.PollInterval),
		subscriptions.WithMaxRetries(m.cfg.MaxRetries),
		subscriptions.WithRetryBackoff(m.cfg.RetryBackoff, m.cfg.MaxRetryBackoff),
	}
}

// buildSubscriber constructs a pericarp Subscriber for a group. wake may be nil
// (pure polling) — the CLI accessors build subscribers without a wake signal
// since they never run the loop.
func (m *Manager) buildSubscriber(g SubscriberGroup, wake <-chan struct{}) (*subscriptions.Subscriber, error) {
	opts := m.baseOptions()
	opts = append(opts, g.Options...)
	if wake != nil {
		opts = append(opts, subscriptions.WithWakeSignal(wake))
	}
	return subscriptions.NewSubscriber(g.Name, m.eventStore, m.checkpoints, recoverHandler(g.Handler), opts...)
}

// recoverHandler converts a handler panic into an error so the subscriber's
// retry/parking machinery handles it like any other failure — a poison event
// that panics is parked after retries instead of crashing the worker goroutine
// and taking the rest of the runtime down with it. The attempt's savepoint
// rolls back its partial writes, same as a returned error.
func recoverHandler(h subscriptions.Handler) subscriptions.Handler {
	return func(ctx context.Context, event domain.EventEnvelope[any]) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("handler panicked on event %s (type %s): %v",
					event.ID, event.EventType, r)
			}
		}()
		return h(ctx, event)
	}
}

// Names returns the registered subscriber group names, sorted for stable CLI
// output.
func (m *Manager) Names() []string {
	names := make([]string, 0, len(m.byName))
	for name := range m.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Subscriber builds a fresh (non-running) Subscriber for the named group, for
// CLI operations like listing/replaying parked events and resetting
// checkpoints. The checkpoint and parking stores are shared, so these
// operations coordinate correctly with a running worker.
func (m *Manager) Subscriber(name string) (*subscriptions.Subscriber, error) {
	g, ok := m.byName[name]
	if !ok {
		return nil, fmt.Errorf("unknown subscriber %q (known: %v)", name, m.Names())
	}
	return m.buildSubscriber(g, nil)
}

// ParkedEvents returns parked (poison) events for one subscriber, or across all
// registered subscribers when subscriber is empty. The result is flat; each
// ParkedEvent carries its own Subscriber. Backs `weos worker parked list`.
func (m *Manager) ParkedEvents(ctx context.Context, subscriber string) ([]subscriptions.ParkedEvent, error) {
	names := m.Names()
	if subscriber != "" {
		if _, ok := m.byName[subscriber]; !ok {
			return nil, fmt.Errorf("unknown subscriber %q (known: %v)", subscriber, names)
		}
		names = []string{subscriber}
	}
	var all []subscriptions.ParkedEvent
	for _, name := range names {
		sub, err := m.Subscriber(name)
		if err != nil {
			return nil, err
		}
		parked, err := sub.ListParked(ctx)
		if err != nil {
			return nil, fmt.Errorf("list parked events for %q: %w", name, err)
		}
		all = append(all, parked...)
	}
	return all, nil
}

// ReplayParked re-runs the handler for one parked event of a subscriber and
// clears the row on success (a failed replay leaves it parked). Backs
// `weos worker parked replay`.
func (m *Manager) ReplayParked(ctx context.Context, subscriber, eventID string) error {
	sub, err := m.Subscriber(subscriber)
	if err != nil {
		return err
	}
	return sub.ReplayParked(ctx, eventID)
}

// Start launches every registered subscriber as a background worker, plus the
// wake source (Postgres listener) and the lag reporter. It returns immediately;
// the workers run until Stop. Calling Start twice is a no-op.
func (m *Manager) Start(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return nil
	}
	// Workers own their own lifetime context, independent of the (short)
	// Fx start-timeout context — they run until Stop cancels them.
	runCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	wake := m.startWakeSource(runCtx)

	for _, g := range m.groups {
		var ch <-chan struct{}
		if wake != nil {
			ch = wake.Subscribe()
		}
		sub, err := m.buildSubscriber(g, ch)
		if err != nil {
			cancel()
			return fmt.Errorf("build subscriber %q: %w", g.Name, err)
		}
		m.wg.Add(1)
		go func(name string, sub *subscriptions.Subscriber) {
			defer m.wg.Done()
			if err := sub.Run(runCtx); err != nil {
				m.logger.Error(runCtx, "worker: subscriber stopped with error",
					"subscriber", name, "error", err)
			}
		}(g.Name, sub)
	}

	if m.cfg.LagLogInterval > 0 && len(m.groups) > 0 {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.logLagLoop(runCtx)
		}()
	}

	m.running = true
	m.logger.Info(runCtx, "worker: background subscribers started",
		"groups", m.Names(), "postgres", m.postgres)
	return nil
}

// startWakeSource picks and runs the wake mechanism for the current database:
// Postgres LISTEN/NOTIFY across processes, or the in-process notifier for
// SQLite. A nil return means pure polling — always correct, just slower.
func (m *Manager) startWakeSource(ctx context.Context) WakeSource {
	if !m.postgres {
		return m.notifier
	}
	listener, err := subscriptions.NewPostgresListener(m.dsn, subscriptions.WithListenerLogger(m.slog))
	if err != nil {
		m.logger.Warn(ctx, "worker: postgres listener unavailable, falling back to polling",
			"error", err)
		return nil
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := listener.Run(ctx); err != nil && ctx.Err() == nil {
			m.logger.Error(ctx, "worker: postgres listener stopped", "error", err)
		}
	}()
	return listener
}

// Stop cancels the workers and waits for them to drain, bounded by ctx. A batch
// already in flight finishes — handlers complete and the checkpoint advances —
// before its subscriber returns, so a clean shutdown never loses work and
// preserves feed order. Delivery is at-least-once in general (exactly-once only
// for handlers that write through the batch transaction via TxFromContext); an
// unclean kill can redeliver the in-flight batch on restart.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return nil
	}
	m.cancel()

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		m.running = false
		m.logger.Info(context.Background(), "worker: background subscribers drained")
		return nil
	case <-ctx.Done():
		// The run context is already canceled; workers will exit once their
		// in-flight batch drains. Report the timeout so a stuck handler is
		// visible rather than silently extending shutdown.
		m.running = false
		return fmt.Errorf("worker: shutdown timed out before all subscribers drained: %w", ctx.Err())
	}
}

// logLagLoop periodically reports each subscriber's checkpoint lag (how far it
// trails the feed head). This is the observability surface for the runtime —
// there is no metrics backend in weos, so lag is emitted as structured logs.
func (m *Manager) logLagLoop(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.LagLogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.LogLag(ctx)
		}
	}
}

// LogLag emits one checkpoint-lag log line per subscriber. Exposed so the CLI
// can report lag on demand as well.
func (m *Manager) LogLag(ctx context.Context) {
	for _, name := range m.Names() {
		sub, err := m.Subscriber(name)
		if err != nil {
			continue
		}
		lag, err := sub.Lag(ctx)
		if err != nil {
			m.logger.Error(ctx, "worker: failed to read checkpoint lag",
				"subscriber", name, "error", err)
			continue
		}
		m.logger.Info(ctx, "worker: checkpoint lag", "subscriber", name, "lag", lag)
	}
}

// subscriberSlog bridges the weos logger into a *slog.Logger for the pericarp
// subscriber runtime, defaulting to slog.Default() if no logger is wired.
func subscriberSlog(logger entities.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logging.NewSlogLogger(logger)
}
