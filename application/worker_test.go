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
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// workerTestEnv bundles a SQLite-backed event store, checkpoint store, parking
// lot, and a projection table for exercising the worker runtime end to end.
type workerTestEnv struct {
	db          *gorm.DB
	eventStore  domain.EventStore
	checkpoints subscriptions.CheckpointStore
	parking     subscriptions.ParkingLot
}

func newWorkerTestEnv(t *testing.T) *workerTestEnv {
	t.Helper()
	// A file-backed DB (not :memory:) so every pooled connection sees the same
	// schema and rows — :memory: gives each connection its own empty database.
	// WAL lets the subscriber's feed reads run concurrently with its batch
	// write transaction; _txlock=immediate makes each write transaction take
	// the write lock upfront so concurrent writers serialize via busy_timeout
	// instead of erroring with "database is locked" on lock upgrade. This
	// mirrors the pragmas the production SQLite provider applies.
	dsn := filepath.Join(t.TempDir(), "worker_test.db") +
		"?_busy_timeout=10000&_journal_mode=WAL&_txlock=immediate"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	eventStore, err := infrastructure.NewGormEventStore(db)
	if err != nil {
		t.Fatalf("event store: %v", err)
	}
	checkpoints, err := subscriptions.NewGormCheckpointStore(db)
	if err != nil {
		t.Fatalf("checkpoint store: %v", err)
	}
	parking, err := subscriptions.NewGormParkingLot(db)
	if err != nil {
		t.Fatalf("parking lot: %v", err)
	}
	if err := db.Exec(
		`CREATE TABLE worker_projection (position INTEGER PRIMARY KEY, aggregate_id TEXT)`,
	).Error; err != nil {
		t.Fatalf("create projection table: %v", err)
	}
	return &workerTestEnv{db: db, eventStore: eventStore, checkpoints: checkpoints, parking: parking}
}

// appendEvents writes n events, one per fresh aggregate (expectedVersion 0), so
// they receive global positions startPos+1 .. startPos+n.
func (e *workerTestEnv) appendEvents(t *testing.T, startPos, n int) {
	t.Helper()
	e.appendEventsTo(t, e.eventStore, startPos, n)
}

// appendEventsTo is appendEvents against a specific store — used to write
// through a NotifyingEventStore so the wake path fires.
func (e *workerTestEnv) appendEventsTo(t *testing.T, store domain.EventStore, startPos, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		pos := startPos + i
		aggID := fmt.Sprintf("agg-%04d", pos)
		env := domain.EventEnvelope[any]{
			ID:          fmt.Sprintf("evt-%04d", pos),
			AggregateID: aggID,
			EventType:   "Test.Happened",
			Payload:     map[string]any{"n": pos},
			Created:     time.Now(),
			SequenceNo:  1,
		}
		if err := store.Append(context.Background(), aggID, 0, env); err != nil {
			t.Fatalf("append event %d: %v", pos, err)
		}
	}
}

func (e *workerTestEnv) projectionCount(t *testing.T) int {
	t.Helper()
	var count int64
	if err := e.db.Raw(`SELECT COUNT(*) FROM worker_projection`).Scan(&count).Error; err != nil {
		t.Fatalf("count projection: %v", err)
	}
	return int(count)
}

// projectionHandler writes one row per event through the batch transaction, so
// the write and the checkpoint advance commit atomically. The position is the
// primary key, so a double-apply would violate the constraint and surface as an
// error rather than passing silently.
func projectionHandler(applied *int64) subscriptions.Handler {
	return func(ctx context.Context, event domain.EventEnvelope[any]) error {
		tx := subscriptions.TxFromContext(ctx)
		if tx == nil {
			return fmt.Errorf("expected batch transaction in context")
		}
		if err := tx.Exec(
			`INSERT INTO worker_projection (position, aggregate_id) VALUES (?, ?)`,
			event.Position, event.AggregateID,
		).Error; err != nil {
			return err
		}
		atomic.AddInt64(applied, 1)
		return nil
	}
}

func newTestManager(t *testing.T, env *workerTestEnv, group SubscriberGroup) *Manager {
	t.Helper()
	cfg := config.Default()
	cfg.DatabaseDSN = "worker_test.db" // SQLite (not Postgres) path
	cfg.Worker.PollInterval = 25 * time.Millisecond
	cfg.Worker.LagLogInterval = 0 // disable the lag goroutine in tests
	m, err := NewManager(ManagerParams{
		EventStore:  env.eventStore,
		Checkpoints: env.checkpoints,
		Parking:     env.parking,
		Notifier:    subscriptions.NewInProcessNotifier(),
		Config:      cfg,
		Logger:      nil, // exercises the slog.Default() fallback
		Groups:      []SubscriberGroup{group},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return m
}

// waitForCount polls until the projection table reaches want rows or the
// deadline elapses.
func waitForCount(t *testing.T, env *workerTestEnv, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if env.projectionCount(t) >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d projection rows, have %d", want, env.projectionCount(t))
}

// TestManager_ResumesFromCheckpointAcrossRestart proves the core epic
// guarantee: a fresh Manager (a stand-in for a restarted process) resumes from
// the committed checkpoint and applies each event exactly once — no skips, no
// double-applies.
func TestManager_ResumesFromCheckpointAcrossRestart(t *testing.T) {
	env := newWorkerTestEnv(t)
	var applied int64
	group := SubscriberGroup{Name: "projection", Handler: projectionHandler(&applied)}

	// First run: process the initial 10 events, then drain cleanly.
	env.appendEvents(t, 0, 10)
	m1 := newTestManager(t, env, group)
	if err := m1.Start(context.Background()); err != nil {
		t.Fatalf("start m1: %v", err)
	}
	waitForCount(t, env, 10)
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m1.Stop(stopCtx); err != nil {
		t.Fatalf("stop m1: %v", err)
	}

	pos, err := env.checkpoints.Position(context.Background(), "projection")
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if pos != 10 {
		t.Fatalf("checkpoint after first run = %d, want 10", pos)
	}

	// Restart: a brand-new Manager over the same stores must pick up only the
	// 5 new events, never reprocessing the first 10.
	env.appendEvents(t, 10, 5)
	m2 := newTestManager(t, env, group)
	if err := m2.Start(context.Background()); err != nil {
		t.Fatalf("start m2: %v", err)
	}
	waitForCount(t, env, 15)
	stopCtx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := m2.Stop(stopCtx2); err != nil {
		t.Fatalf("stop m2: %v", err)
	}

	if got := env.projectionCount(t); got != 15 {
		t.Fatalf("projection rows = %d, want 15 (no skips, no double-applies)", got)
	}
	// Positions 1..15 each present exactly once (PRIMARY KEY guarantees no
	// duplicates; this checks for gaps).
	var distinct int64
	if err := env.db.Raw(`SELECT COUNT(DISTINCT position) FROM worker_projection`).Scan(&distinct).Error; err != nil {
		t.Fatalf("distinct count: %v", err)
	}
	if distinct != 15 {
		t.Fatalf("distinct positions = %d, want 15", distinct)
	}
}

// TestManager_ParksPoisonEventAndKeepsFlowing verifies that an event a handler
// can never process is parked after its retries and the events behind it still
// get applied — one bad event does not stall the group.
func TestManager_ParksPoisonEventAndKeepsFlowing(t *testing.T) {
	env := newWorkerTestEnv(t)
	var applied int64
	handler := func(ctx context.Context, event domain.EventEnvelope[any]) error {
		if event.Position == 3 {
			return fmt.Errorf("deterministic failure on position 3")
		}
		return projectionHandler(&applied)(ctx, event)
	}
	group := SubscriberGroup{
		Name:    "poison",
		Handler: handler,
		Options: []subscriptions.SubscriberOption{
			// Park quickly so the test does not wait out the default backoff.
			subscriptions.WithMaxRetries(1),
			subscriptions.WithRetryBackoff(time.Millisecond, 2*time.Millisecond),
		},
	}

	env.appendEvents(t, 0, 5)
	m := newTestManager(t, env, group)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	// 4 of 5 events apply (position 3 is parked).
	waitForCount(t, env, 4)
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if got := env.projectionCount(t); got != 4 {
		t.Fatalf("projection rows = %d, want 4 (position 3 parked)", got)
	}
	// Position 3 must be absent; the checkpoint advanced past it.
	var hasThree int64
	if err := env.db.Raw(`SELECT COUNT(*) FROM worker_projection WHERE position = 3`).Scan(&hasThree).Error; err != nil {
		t.Fatalf("query position 3: %v", err)
	}
	if hasThree != 0 {
		t.Fatalf("position 3 should be parked, not projected")
	}

	sub, err := m.Subscriber("poison")
	if err != nil {
		t.Fatalf("subscriber: %v", err)
	}
	parked, err := sub.ListParked(context.Background())
	if err != nil {
		t.Fatalf("list parked: %v", err)
	}
	if len(parked) != 1 || parked[0].Position != 3 {
		t.Fatalf("expected one parked event at position 3, got %+v", parked)
	}
}

// TestManager_HandlerPanicIsParkedNotFatal proves a panicking handler is
// converted to a parked event (via recoverHandler) instead of crashing the
// worker goroutine and the rest of the runtime.
func TestManager_HandlerPanicIsParkedNotFatal(t *testing.T) {
	env := newWorkerTestEnv(t)
	var applied int64
	handler := func(ctx context.Context, event domain.EventEnvelope[any]) error {
		if event.Position == 2 {
			panic("boom")
		}
		return projectionHandler(&applied)(ctx, event)
	}
	group := SubscriberGroup{
		Name:    "panicky",
		Handler: handler,
		Options: []subscriptions.SubscriberOption{
			subscriptions.WithMaxRetries(1),
			subscriptions.WithRetryBackoff(time.Millisecond, 2*time.Millisecond),
		},
	}

	env.appendEvents(t, 0, 3)
	m := newTestManager(t, env, group)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForCount(t, env, 2) // positions 1 and 3 apply; 2 panics and parks
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	sub, err := m.Subscriber("panicky")
	if err != nil {
		t.Fatalf("subscriber: %v", err)
	}
	parked, err := sub.ListParked(context.Background())
	if err != nil {
		t.Fatalf("list parked: %v", err)
	}
	if len(parked) != 1 || parked[0].Position != 2 {
		t.Fatalf("expected the panicking event (position 2) parked, got %+v", parked)
	}
}

// capturingLogger captures emitted log lines so tests can assert on the
// runtime's observability output (e.g. checkpoint-lag lines).
type capturingLogger struct {
	mu    sync.Mutex
	lines []recordedLine
}

type recordedLine struct {
	level  string
	msg    string
	fields []any
}

func (l *capturingLogger) record(level, msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, recordedLine{level: level, msg: msg, fields: fields})
}

func (l *capturingLogger) Debug(_ context.Context, m string, f ...any) { l.record("debug", m, f) }
func (l *capturingLogger) Info(_ context.Context, m string, f ...any)  { l.record("info", m, f) }
func (l *capturingLogger) Warn(_ context.Context, m string, f ...any)  { l.record("warn", m, f) }
func (l *capturingLogger) Error(_ context.Context, m string, f ...any) { l.record("error", m, f) }

// field returns the value logged for key on the first line with the given
// message, treating fields as flattened key/value pairs.
func (l *capturingLogger) field(msg, key string) (any, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ln := range l.lines {
		if ln.msg != msg {
			continue
		}
		for i := 0; i+1 < len(ln.fields); i += 2 {
			if k, ok := ln.fields[i].(string); ok && k == key {
				return ln.fields[i+1], true
			}
		}
	}
	return nil, false
}

// hasLevel reports whether a line with the given level and message was recorded.
func (l *capturingLogger) hasLevel(level, msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ln := range l.lines {
		if ln.level == level && ln.msg == msg {
			return true
		}
	}
	return false
}

// sqliteWorkerConfig returns a Config on the (non-Postgres) SQLite worker path
// with a tight poll interval and lag logging disabled, suitable for tests.
func sqliteWorkerConfig() config.Config {
	cfg := config.Default()
	cfg.DatabaseDSN = "worker_test.db"
	cfg.Worker.PollInterval = 25 * time.Millisecond
	cfg.Worker.LagLogInterval = 0
	return cfg
}

// tableHandler writes one row per event into the named table through the batch
// transaction, optionally failing on poisonPos to simulate a poison event.
func tableHandler(table string, poisonPos int64) subscriptions.Handler {
	return func(ctx context.Context, event domain.EventEnvelope[any]) error {
		if poisonPos > 0 && event.Position == poisonPos {
			return fmt.Errorf("deterministic failure on position %d", poisonPos)
		}
		tx := subscriptions.TxFromContext(ctx)
		if tx == nil {
			return fmt.Errorf("expected batch transaction in context")
		}
		return tx.Exec(
			fmt.Sprintf(`INSERT INTO %s (position, aggregate_id) VALUES (?, ?)`, table),
			event.Position, event.AggregateID,
		).Error
	}
}

func tableCount(t *testing.T, db *gorm.DB, table string) int {
	t.Helper()
	var n int64
	if err := db.Raw(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)).Scan(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return int(n)
}

func waitForTableCount(t *testing.T, db *gorm.DB, table string, want int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if tableCount(t, db, table) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %d rows in %s, have %d", within, want, table, tableCount(t, db, table))
}

// TestManager_ReportsCheckpointLag covers the runtime's only observability
// surface (a stated acceptance criterion): Lag reflects how far a subscriber
// trails the feed head, and LogLag emits a structured line per group.
func TestManager_ReportsCheckpointLag(t *testing.T) {
	env := newWorkerTestEnv(t)
	rec := &capturingLogger{}
	var applied int64
	group := SubscriberGroup{Name: "lag", Handler: projectionHandler(&applied)}

	cfg := sqliteWorkerConfig()
	m, err := NewManager(ManagerParams{
		EventStore:  env.eventStore,
		Checkpoints: env.checkpoints,
		Parking:     env.parking,
		Notifier:    subscriptions.NewInProcessNotifier(),
		Config:      cfg,
		Logger:      rec,
		Groups:      []SubscriberGroup{group},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Before processing: lag equals the full backlog.
	env.appendEvents(t, 0, 7)
	sub, err := m.Subscriber("lag")
	if err != nil {
		t.Fatalf("subscriber: %v", err)
	}
	lag, err := sub.Lag(context.Background())
	if err != nil {
		t.Fatalf("lag: %v", err)
	}
	if lag != 7 {
		t.Fatalf("lag before processing = %d, want 7", lag)
	}

	// After draining: lag is zero, and LogLag reports it.
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForCount(t, env, 7)
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	lag, err = sub.Lag(context.Background())
	if err != nil {
		t.Fatalf("lag after drain: %v", err)
	}
	if lag != 0 {
		t.Fatalf("lag after drain = %d, want 0", lag)
	}

	m.LogLag(context.Background())
	v, ok := rec.field("worker: checkpoint lag", "subscriber")
	if !ok || v != "lag" {
		t.Fatalf("expected a checkpoint-lag log line for subscriber 'lag', got %v (present=%v)", v, ok)
	}
	if lagVal, ok := rec.field("worker: checkpoint lag", "lag"); !ok || lagVal != int64(0) {
		t.Fatalf("expected logged lag 0, got %v (present=%v)", lagVal, ok)
	}
}

// TestManager_WakesOnCommitBeforePollInterval proves the wake path is live: with
// a 10s poll interval, a commit through the NotifyingEventStore must be picked
// up almost immediately (well inside the poll interval) rather than after it.
func TestManager_WakesOnCommitBeforePollInterval(t *testing.T) {
	env := newWorkerTestEnv(t)
	notifier := subscriptions.NewInProcessNotifier()
	// Mirror DecorateNotifyingEventStore: wrap the store so Append wakes workers.
	wrapped := subscriptions.NewNotifyingEventStore(env.eventStore, notifier.Notify)

	cfg := sqliteWorkerConfig()
	cfg.Worker.PollInterval = 10 * time.Second // polling cannot account for progress

	var applied int64
	m, err := NewManager(ManagerParams{
		EventStore:  wrapped,
		Checkpoints: env.checkpoints,
		Parking:     env.parking,
		Notifier:    notifier,
		Config:      cfg,
		Logger:      nil,
		Groups:      []SubscriberGroup{{Name: "wake", Handler: projectionHandler(&applied)}},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Stop(stopCtx)
	}()

	// Append after Start, through the wrapped store, so the commit fires a wake.
	env.appendEventsTo(t, wrapped, 0, 3)
	// If wake works, this completes in well under the 10s poll interval.
	waitForCount(t, env, 3)
}

// TestManager_RollsBackPartialWriteOnFailure converts the recoverHandler /
// projection-atomicity comment into an enforced invariant: a handler that writes
// a row and then fails leaves no row behind — the attempt's savepoint discards
// the partial write while the events around it still apply.
func TestManager_RollsBackPartialWriteOnFailure(t *testing.T) {
	env := newWorkerTestEnv(t)
	var applied int64
	handler := func(ctx context.Context, event domain.EventEnvelope[any]) error {
		tx := subscriptions.TxFromContext(ctx)
		if tx == nil {
			return fmt.Errorf("expected batch transaction in context")
		}
		if event.Position == 2 {
			// Write a partial row, then fail: the savepoint must roll it back.
			if err := tx.Exec(
				`INSERT INTO worker_projection (position, aggregate_id) VALUES (?, ?)`,
				event.Position, "partial",
			).Error; err != nil {
				return err
			}
			return fmt.Errorf("fail after partial write at position 2")
		}
		return projectionHandler(&applied)(ctx, event)
	}
	group := SubscriberGroup{
		Name:    "partial",
		Handler: handler,
		Options: []subscriptions.SubscriberOption{
			subscriptions.WithMaxRetries(1),
			subscriptions.WithRetryBackoff(time.Millisecond, 2*time.Millisecond),
		},
	}

	env.appendEvents(t, 0, 3)
	m := newTestManager(t, env, group)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForCount(t, env, 2) // positions 1 and 3 apply
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	var partial int64
	if err := env.db.Raw(
		`SELECT COUNT(*) FROM worker_projection WHERE position = 2`,
	).Scan(&partial).Error; err != nil {
		t.Fatalf("query position 2: %v", err)
	}
	if partial != 0 {
		t.Fatalf("partial write at position 2 should have been rolled back to the savepoint")
	}
}

// TestManager_GroupsAreIsolated verifies the core grouping guarantee: a poison
// event in one group does not stall another — each group advances its own
// checkpoint independently.
func TestManager_GroupsAreIsolated(t *testing.T) {
	env := newWorkerTestEnv(t)
	if err := env.db.Exec(
		`CREATE TABLE proj_clean (position INTEGER PRIMARY KEY, aggregate_id TEXT)`,
	).Error; err != nil {
		t.Fatalf("create proj_clean: %v", err)
	}
	if err := env.db.Exec(
		`CREATE TABLE proj_poison (position INTEGER PRIMARY KEY, aggregate_id TEXT)`,
	).Error; err != nil {
		t.Fatalf("create proj_poison: %v", err)
	}

	clean := SubscriberGroup{Name: "clean", Handler: tableHandler("proj_clean", 0)}
	poison := SubscriberGroup{
		Name:    "poison",
		Handler: tableHandler("proj_poison", 3),
		Options: []subscriptions.SubscriberOption{
			subscriptions.WithMaxRetries(1),
			subscriptions.WithRetryBackoff(time.Millisecond, 2*time.Millisecond),
		},
	}

	cfg := sqliteWorkerConfig()
	m, err := NewManager(ManagerParams{
		EventStore:  env.eventStore,
		Checkpoints: env.checkpoints,
		Parking:     env.parking,
		Notifier:    subscriptions.NewInProcessNotifier(),
		Config:      cfg,
		Logger:      nil,
		Groups:      []SubscriberGroup{clean, poison},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	env.appendEvents(t, 0, 5)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	// The clean group applies all 5; the poison group applies 4 (position 3 parks).
	waitForTableCount(t, env.db, "proj_clean", 5, 5*time.Second)
	waitForTableCount(t, env.db, "proj_poison", 4, 5*time.Second)
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// The clean group is unaffected by the other group's poison event.
	if got := tableCount(t, env.db, "proj_clean"); got != 5 {
		t.Fatalf("clean group applied %d rows, want 5 (must be isolated from poison group)", got)
	}
	cleanPos, err := env.checkpoints.Position(context.Background(), "clean")
	if err != nil {
		t.Fatalf("clean checkpoint: %v", err)
	}
	if cleanPos != 5 {
		t.Fatalf("clean checkpoint = %d, want 5", cleanPos)
	}
}

// TestNewManager_RejectsInvalidGroups checks the construction-time validation
// that keeps a misconfigured group set from producing a half-built runtime.
func TestNewManager_RejectsInvalidGroups(t *testing.T) {
	env := newWorkerTestEnv(t)
	base := func(groups []SubscriberGroup) ManagerParams {
		return ManagerParams{
			EventStore:  env.eventStore,
			Checkpoints: env.checkpoints,
			Parking:     env.parking,
			Notifier:    subscriptions.NewInProcessNotifier(),
			Config:      sqliteWorkerConfig(),
			Groups:      groups,
		}
	}
	noop := func(context.Context, domain.EventEnvelope[any]) error { return nil }

	cases := map[string][]SubscriberGroup{
		"empty name":     {{Name: "", Handler: noop}},
		"nil handler":    {{Name: "x", Handler: nil}},
		"duplicate name": {{Name: "x", Handler: noop}, {Name: "x", Handler: noop}},
	}
	for name, groups := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewManager(base(groups)); err == nil {
				t.Fatalf("expected NewManager to reject %s", name)
			}
		})
	}
}

// TestManager_ListReplayAndLogParked covers the operator-facing surface
// (story #368): a parked event is listed, replaying it after the cause is fixed
// clears the row and applies the event, and parking is logged at error level
// (suitable for alerting).
func TestManager_ListReplayAndLogParked(t *testing.T) {
	env := newWorkerTestEnv(t)
	rec := &capturingLogger{}
	var failing atomic.Bool
	failing.Store(true)
	var applied int64
	handler := func(ctx context.Context, event domain.EventEnvelope[any]) error {
		if event.Position == 2 && failing.Load() {
			return fmt.Errorf("temporary failure on position 2")
		}
		return projectionHandler(&applied)(ctx, event)
	}
	group := SubscriberGroup{
		Name:    "grp",
		Handler: handler,
		Options: []subscriptions.SubscriberOption{
			subscriptions.WithMaxRetries(1),
			subscriptions.WithRetryBackoff(time.Millisecond, 2*time.Millisecond),
		},
	}

	cfg := sqliteWorkerConfig()
	m, err := NewManager(ManagerParams{
		EventStore:  env.eventStore,
		Checkpoints: env.checkpoints,
		Parking:     env.parking,
		Notifier:    subscriptions.NewInProcessNotifier(),
		Config:      cfg,
		Logger:      rec,
		Groups:      []SubscriberGroup{group},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	env.appendEvents(t, 0, 3)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForCount(t, env, 2) // positions 1 and 3 apply; 2 parks
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// AC: parking surfaces in logs at error level (for alerting).
	if v, ok := rec.field("event parked after retries exhausted", "subscriber"); !ok || v != "grp" {
		t.Fatalf("expected an error-level parked log for subscriber 'grp', got %v (present=%v)", v, ok)
	}
	if !rec.hasLevel("error", "event parked after retries exhausted") {
		t.Fatalf("parked event must be logged at error level")
	}

	// AC: list parked events.
	parked, err := m.ParkedEvents(context.Background(), "")
	if err != nil {
		t.Fatalf("parked events: %v", err)
	}
	if len(parked) != 1 || parked[0].Position != 2 || parked[0].Subscriber != "grp" {
		t.Fatalf("expected one parked event (pos 2, subscriber grp), got %+v", parked)
	}
	if _, err := m.ParkedEvents(context.Background(), "nope"); err == nil {
		t.Fatalf("expected error listing parked events for unknown subscriber")
	}

	// Replaying before the cause is fixed must fail and leave the row parked,
	// so an operator does not believe a still-poison event was cleared.
	if err := m.ReplayParked(context.Background(), "grp", parked[0].EventID); err == nil {
		t.Fatalf("expected replay to fail while the handler still fails")
	}
	stillParked, err := m.ParkedEvents(context.Background(), "grp")
	if err != nil {
		t.Fatalf("parked after failed replay: %v", err)
	}
	if len(stillParked) != 1 || stillParked[0].Position != 2 {
		t.Fatalf("a failed replay must leave the event parked, got %+v", stillParked)
	}

	// AC: replay after the cause is fixed clears the row and applies the event.
	failing.Store(false)
	if err := m.ReplayParked(context.Background(), "grp", parked[0].EventID); err != nil {
		t.Fatalf("replay: %v", err)
	}
	after, err := m.ParkedEvents(context.Background(), "grp")
	if err != nil {
		t.Fatalf("parked after replay: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("expected no parked events after successful replay, got %+v", after)
	}
	var hasTwo int64
	if err := env.db.Raw(`SELECT COUNT(*) FROM worker_projection WHERE position = 2`).Scan(&hasTwo).Error; err != nil {
		t.Fatalf("query position 2: %v", err)
	}
	if hasTwo != 1 {
		t.Fatalf("replayed event should have applied position 2 to the projection")
	}
}

// TestManager_ParkedEventsAggregatesAcrossSubscribers covers the multi-group
// path of ParkedEvents: an empty subscriber filter returns every group's parked
// events flat, each carrying its own Subscriber, while a filter returns only
// that group's.
func TestManager_ParkedEventsAggregatesAcrossSubscribers(t *testing.T) {
	env := newWorkerTestEnv(t)
	if err := env.db.Exec(
		`CREATE TABLE proj_a (position INTEGER PRIMARY KEY, aggregate_id TEXT)`,
	).Error; err != nil {
		t.Fatalf("create proj_a: %v", err)
	}
	if err := env.db.Exec(
		`CREATE TABLE proj_b (position INTEGER PRIMARY KEY, aggregate_id TEXT)`,
	).Error; err != nil {
		t.Fatalf("create proj_b: %v", err)
	}

	fastPark := []subscriptions.SubscriberOption{
		subscriptions.WithMaxRetries(1),
		subscriptions.WithRetryBackoff(time.Millisecond, 2*time.Millisecond),
	}
	// "a" poisons position 2, "b" poisons position 4 — distinct rows per group.
	groupA := SubscriberGroup{Name: "a", Handler: tableHandler("proj_a", 2), Options: fastPark}
	groupB := SubscriberGroup{Name: "b", Handler: tableHandler("proj_b", 4), Options: fastPark}

	m, err := NewManager(ManagerParams{
		EventStore:  env.eventStore,
		Checkpoints: env.checkpoints,
		Parking:     env.parking,
		Notifier:    subscriptions.NewInProcessNotifier(),
		Config:      sqliteWorkerConfig(),
		Groups:      []SubscriberGroup{groupA, groupB},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	env.appendEvents(t, 0, 5)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Each group applies 4 of 5 (one poisoned position each).
	waitForTableCount(t, env.db, "proj_a", 4, 5*time.Second)
	waitForTableCount(t, env.db, "proj_b", 4, 5*time.Second)
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	all, err := m.ParkedEvents(context.Background(), "")
	if err != nil {
		t.Fatalf("aggregate parked: %v", err)
	}
	bySub := map[string]subscriptions.ParkedEvent{}
	for _, p := range all {
		bySub[p.Subscriber] = p
	}
	if len(all) != 2 || len(bySub) != 2 {
		t.Fatalf("expected one parked event per subscriber (2 total, distinct), got %+v", all)
	}
	if bySub["a"].Position != 2 || bySub["b"].Position != 4 {
		t.Fatalf("parked positions/subscribers mismatched: %+v", bySub)
	}

	// Filtering returns only that subscriber's parked events.
	onlyA, err := m.ParkedEvents(context.Background(), "a")
	if err != nil {
		t.Fatalf("filter a: %v", err)
	}
	if len(onlyA) != 1 || onlyA[0].Subscriber != "a" {
		t.Fatalf("expected only subscriber 'a' rows, got %+v", onlyA)
	}
}

var _ entities.Logger = (*capturingLogger)(nil)
