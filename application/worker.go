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
	// Truncate optionally clears the group's projection so a checkpoint reset
	// rebuilds it from empty (e.g. the Oxigraph store's Clear). nil means the
	// group has no separate projection to clear — display-values, for instance,
	// writes denormalized columns into other types' rows rather than owning a
	// table — and `worker checkpoint reset --truncate` reports that.
	Truncate func(context.Context) error
	// StartAtHead initializes a group that has never run before at the
	// current feed head instead of position 0, so enabling it on an instance
	// with existing history does not replay that history through the handler.
	// Only a missing checkpoint row is initialized — an existing row is never
	// moved, so an explicit `worker checkpoint reset` (which creates the row
	// at 0) remains the opt-in backfill path. Groups whose head
	// initialization fails are not started that run (fail closed): for an
	// expensive handler like consolidation, silently falling back to a
	// full-history replay is worse than sitting out one process lifetime.
	StartAtHead bool
}

// WakeSource hands out wake channels so idle subscribers react to new commits
// instead of waiting out the poll interval. Each subscriber gets its own
// channel (channel receives are point-to-point). Implemented by pericarp's
// InProcessNotifier (SQLite / single process) and PostgresListener (cross
// process via LISTEN/NOTIFY).
type WakeSource interface {
	Subscribe() <-chan struct{}
}

// EnsureCheckpointFunc creates the named subscriber's checkpoint row at the
// given position only when no row exists yet; an existing row — including one
// an operator explicitly reset to 0 — is never moved. It backs
// SubscriberGroup.StartAtHead. The pericarp CheckpointStore interface cannot
// express this (Position returns 0 both for "unknown" and "reset to 0"), so
// the gorm layer provides it against the checkpoint table directly.
type EnsureCheckpointFunc func(ctx context.Context, subscriber string, position int64) error

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
	ensure      EnsureCheckpointFunc
	cfg         config.WorkerConfig
	postgres    bool
	dsn         string
	logger      entities.Logger
	slog        *slog.Logger

	groups         []SubscriberGroup
	byName         map[string]SubscriberGroup
	rebuildOnStart []string // groups to reset+truncate before launching

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
	// RebuildOnStart names groups whose checkpoint is reset (and projection
	// truncated) before the workers launch — the OXIGRAPH_REBUILD path, now
	// expressed as a checkpoint reset so there is one replay mechanism.
	RebuildOnStart []string
	// EnsureCheckpoint backs SubscriberGroup.StartAtHead. nil is only valid
	// when no group sets StartAtHead — such a group would then never start.
	EnsureCheckpoint EnsureCheckpointFunc
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
		eventStore:     p.EventStore,
		checkpoints:    p.Checkpoints,
		parking:        p.Parking,
		notifier:       p.Notifier,
		ensure:         p.EnsureCheckpoint,
		cfg:            p.Config.Worker,
		postgres:       p.Config.IsPostgres(),
		dsn:            p.Config.DatabaseDSN,
		logger:         logger,
		slog:           subscriberSlog(logger),
		groups:         p.Groups,
		byName:         byName,
		rebuildOnStart: p.RebuildOnStart,
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

// ResetCheckpoint resets a subscriber's checkpoint to 0 so it replays all
// history — the one mechanism for rebuild, recovery, and backfill. When
// truncate is set, the group's projection is cleared first (so the replay
// repopulates from empty); a group with no Truncate action rejects --truncate.
// Replay is incremental and resumable: it uses the same batch/checkpoint cycle
// as live processing, so interrupting and restarting continues from where it
// left off. Backs `weos worker checkpoint reset`.
func (m *Manager) ResetCheckpoint(ctx context.Context, subscriber string, truncate bool) error {
	g, ok := m.byName[subscriber]
	if !ok {
		return fmt.Errorf("unknown subscriber %q (known: %v)", subscriber, m.Names())
	}
	if truncate {
		if g.Truncate == nil {
			return fmt.Errorf("subscriber %q has no projection to truncate; reset without --truncate", subscriber)
		}
		if err := g.Truncate(ctx); err != nil {
			return fmt.Errorf("truncate projection for %q: %w", subscriber, err)
		}
	}
	sub, err := m.buildSubscriber(g, nil)
	if err != nil {
		return err
	}
	return sub.ResetCheckpoint(ctx, 0)
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

	// Initialize StartAtHead groups before launching, so a group that has
	// never run begins at the feed head instead of replaying all history. A
	// group whose initialization fails is skipped this run — see
	// SubscriberGroup.StartAtHead.
	skip := m.initStartAtHeadGroups(runCtx)

	// Rebuild requested groups before launching: reset their checkpoint to 0
	// (and truncate their projection) so the initial catch-up replays history
	// from the start. Doing it here — before the subscriber loop launches —
	// avoids racing a clear against the worker's own writes.
	for _, name := range m.rebuildOnStart {
		m.logger.Info(runCtx, "worker: rebuilding projection from event 0", "subscriber", name)
		if err := m.ResetCheckpoint(runCtx, name, true); err != nil {
			// Never fail startup over a rebuild — log and continue; the
			// subscriber still runs from its existing checkpoint.
			m.logger.Error(runCtx, "worker: rebuild reset failed", "subscriber", name, "error", err)
		}
	}

	wake := m.startWakeSource(runCtx)

	for _, g := range m.groups {
		if skip[g.Name] {
			continue
		}
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

// initStartAtHeadGroups creates the checkpoint row at the current feed head
// for every StartAtHead group that has never run before (an existing row is
// never moved — see EnsureCheckpointFunc). It returns the names of groups
// whose initialization failed; those must not be launched this run, because
// their first Acquire would create the checkpoint at 0 and replay all history
// through a handler that was explicitly declared too expensive for that.
func (m *Manager) initStartAtHeadGroups(ctx context.Context) map[string]bool {
	skip := make(map[string]bool)
	for _, g := range m.groups {
		if !g.StartAtHead {
			continue
		}
		if err := m.initCheckpointAtHead(ctx, g.Name); err != nil {
			m.logger.Error(ctx, "worker: checkpoint head initialization failed; "+
				"subscriber will not start this run", "subscriber", g.Name, "error", err)
			skip[g.Name] = true
		}
	}
	return skip
}

func (m *Manager) initCheckpointAtHead(ctx context.Context, name string) error {
	if m.ensure == nil {
		return fmt.Errorf("no EnsureCheckpoint wired for StartAtHead group %q", name)
	}
	head, err := m.eventStore.HeadPosition(ctx)
	if err != nil {
		return fmt.Errorf("read feed head: %w", err)
	}
	return m.ensure(ctx, name, head)
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
		// visible rather than silently extending shutdown. Leave m.running
		// true: the goroutines are still draining on m.wg, so flipping it
		// false would let a later Start launch a duplicate set of subscribers
		// in the same process. Holding it true keeps Start a no-op and lets
		// Stop be retried — it returns cleanly once the in-flight batch drains.
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
