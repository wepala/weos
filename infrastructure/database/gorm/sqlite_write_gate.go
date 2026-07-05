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

package gorm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	gosqlite "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// SQLite allows one writer per database file. With _txlock=immediate every
// transaction takes the write lock at BEGIN, so N goroutines beginning
// transactions on separate pooled connections race the file lock inside
// SQLite — and the loser of a lock-upgrade race fails *immediately* with
// SQLITE_BUSY, before busy_timeout is even consulted (#421). The write gate
// moves that queue into Go: at most one write transaction is in flight per
// database file, waiters queue on a semaphore (ctx-cancellable, inspectable
// in a goroutine dump), and in-process SQLITE_BUSY becomes structurally
// impossible. Reads never touch the gate — only BeginTx acquires it — so a
// write burst cannot starve reads or server boot the way the reverted
// MaxOpenConns(1) approach did (#425).
//
// The gate is installed at the gorm.ConnPool seam by DialectorForDSN, which
// means every consumer of the shared *gorm.DB — weos repositories, pericarp's
// event store, checkpoint store and parking lot, and the ADK session store's
// independent handle — inherits it without opting in.
//
// Invariant for subscriber handlers: writes made while a batch transaction is
// open must join it via subscriptions.TxFromContext (see the writeDB helpers
// on projectionManager and ResourceRepository). A handler write that opens a
// fresh transaction instead bypasses the gate (see BeginTx) and contends with
// its own batch inside SQLite — a bounded busy_timeout failure rather than a
// wedge, but still a failure.

const (
	// sqliteBusyCode is SQLite's primary result code for "database is locked"
	// (SQLITE_BUSY). Stable since SQLite 3.0.
	sqliteBusyCode = 5

	// busyRetryMaxAttempts bounds the retry-on-BUSY loop in BeginTx. Retry is
	// belt-and-suspenders for contention the gate cannot see (another process,
	// or a second in-process pool on the same file).
	busyRetryMaxAttempts = 5

	// busyRetryInitialDelay is the first retry delay; it doubles per attempt
	// up to busyRetryMaxDelay.
	busyRetryInitialDelay = 50 * time.Millisecond
	busyRetryMaxDelay     = 2 * time.Second

	// writeGateWarnAfter is how long a writer may queue at the gate before a
	// warning is emitted (stderr — the logger doesn't exist when the dialector
	// is built, and stdout carries the MCP stdio protocol). A healthy gate
	// holds for milliseconds; a wait this long means a holder is stuck or an
	// LLM-latency-bound batch is monopolizing the writer slot.
	writeGateWarnAfter = 10 * time.Second
)

// writeGate is a binary semaphore serializing write-intent transactions on
// one SQLite file. Lock is context-aware so a queued writer honors its
// caller's deadline/cancellation instead of waiting indefinitely.
type writeGate struct {
	ch chan struct{}
}

func newWriteGate() *writeGate {
	return &writeGate{ch: make(chan struct{}, 1)}
}

func (g *writeGate) Lock(ctx context.Context) error {
	select {
	case g.ch <- struct{}{}:
		return nil
	default:
	}
	// Slow path: the gate is held. Warn once if the wait runs long — silence
	// here would make a stuck holder look like a slow query — and wrap a
	// cancellation so the operator can tell "died queued at the write gate"
	// from an ordinary timeout.
	warn := time.NewTimer(writeGateWarnAfter)
	defer warn.Stop()
	for {
		select {
		case g.ch <- struct{}{}:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("queued at the sqlite write gate: %w", ctx.Err())
		case <-warn.C:
			fmt.Fprintf(os.Stderr,
				"weos: sqlite write gate: writer queued for over %s — the current holder is stuck or long-running\n",
				writeGateWarnAfter)
		}
	}
}

func (g *writeGate) Unlock() {
	<-g.ch
}

// writeGates shares one gate per canonical database file, so two *gorm.DB
// handles opened on the same file (the main DB and the ADK session store when
// AGENT_SESSION_DSN is unset) serialize against each other, while distinct
// files get independent gates — matching SQLite's own per-file locking.
var (
	writeGatesMu sync.Mutex
	writeGates   = map[string]*writeGate{}
)

func writeGateFor(dsn string) *writeGate {
	key := sqliteFileKey(dsn)
	writeGatesMu.Lock()
	defer writeGatesMu.Unlock()
	if g, ok := writeGates[key]; ok {
		return g
	}
	g := newWriteGate()
	writeGates[key] = g
	return g
}

// sqliteFileKey canonicalizes a SQLite DSN to its file path (best-effort: no
// symlink resolution — two spellings resolving to one inode via symlinks
// would get separate gates, which today's single-DSN configuration never
// produces).
func sqliteFileKey(dsn string) string {
	p := strings.TrimPrefix(dsn, "file:")
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// busyRetryObserver is a test seam: when set (SetBusyRetryObserver), it is
// called once per gated BeginTx attempt with the attempt number and its
// outcome. Production leaves it nil.
var busyRetryObserver atomic.Pointer[func(attempt int, err error)]

// SetBusyRetryObserver installs a hook called once per gated BeginTx attempt
// (err is nil on the attempt that succeeded). Test seam for asserting retry
// behavior end to end; pass nil to clear.
func SetBusyRetryObserver(fn func(attempt int, err error)) {
	if fn == nil {
		busyRetryObserver.Store(nil)
		return
	}
	busyRetryObserver.Store(&fn)
}

func notifyBusyObserver(attempt int, err error) {
	if fn := busyRetryObserver.Load(); fn != nil {
		(*fn)(attempt, err)
	}
}

// isSQLiteBusy reports whether err is (or wraps) a glebarez/go-sqlite error
// whose primary result code is SQLITE_BUSY (extended codes like
// SQLITE_BUSY_SNAPSHOT share the low byte). This is the only error class the
// retry loop treats as transient.
func isSQLiteBusy(err error) bool {
	var e *gosqlite.Error
	return errors.As(err, &e) && e.Code()&0xff == sqliteBusyCode
}

// newGatedSQLiteDialector opens the raw *sql.DB itself and hands the glebarez
// dialector a pre-wrapped ConnPool via its Conn field (its Initialize then
// skips sql.Open and installs the Conn as-is). Building the concrete
// *sqlite.Dialector — rather than decorating the gorm.Dialector interface —
// keeps its full method set visible to gorm's optional-interface assertions:
// an interface-embedding wrapper hides SavePoint/RollbackTo
// (SavePointerDialectorInterface) and Translate (ErrorTranslator), which
// breaks every savepoint (pericarp brackets each subscriber handler attempt
// in one) with "unsupported driver".
//
// augmentedDSN carries the worker pragmas; rawDSN keys the per-file gate.
func newGatedSQLiteDialector(augmentedDSN, rawDSN string) gorm.Dialector {
	sqlDB, err := sql.Open(sqlite.DriverName, augmentedDSN)
	if err != nil {
		// Effectively unreachable: the driver is registered by the
		// glebarez/go-sqlite import above, and sql.Open defers DSN parsing
		// to connection time. Fail fast at gorm.Open rather than silently
		// degrade to an ungated pool — a working-but-ungated database would
		// reintroduce the SQLITE_BUSY class this gate exists to eliminate.
		return failingDialector{err: fmt.Errorf("sqlite write gate: open %q: %w", rawDSN, err)}
	}
	return &sqlite.Dialector{
		DriverName: sqlite.DriverName,
		DSN:        augmentedDSN,
		Conn:       &gatedConnPool{db: sqlDB, gate: writeGateFor(rawDSN)},
	}
}

// failingDialector surfaces a construction error through gorm.Open (which
// calls Initialize first and returns its error before any other method is
// used). The remaining gorm.Dialector methods are dead stubs.
type failingDialector struct{ err error }

func (d failingDialector) Name() string                                        { return "sqlite" }
func (d failingDialector) Initialize(*gorm.DB) error                           { return d.err }
func (d failingDialector) Migrator(*gorm.DB) gorm.Migrator                     { return nil }
func (d failingDialector) DataTypeOf(*schema.Field) string                     { return "" }
func (d failingDialector) DefaultValueOf(*schema.Field) clause.Expression      { return nil }
func (d failingDialector) BindVarTo(clause.Writer, *gorm.Statement, any)       {}
func (d failingDialector) QuoteTo(clause.Writer, string)                       {}
func (d failingDialector) Explain(sql string, vars ...any) string              { return sql }

// gatedConnPool implements gorm.ConnPool over the raw *sql.DB. Reads and
// non-transactional statements delegate straight through — they never queue.
// Only BeginTx (gorm.ConnPoolBeginner) acquires the gate, and it holds zero
// pool connections while waiting.
type gatedConnPool struct {
	db   *sql.DB
	gate *writeGate
}

func (p *gatedConnPool) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return p.db.PrepareContext(ctx, query)
}

func (p *gatedConnPool) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return p.db.ExecContext(ctx, query, args...)
}

func (p *gatedConnPool) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return p.db.QueryContext(ctx, query, args...)
}

func (p *gatedConnPool) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return p.db.QueryRowContext(ctx, query, args...)
}

// Ping keeps gorm.Open's automatic connectivity check working.
func (p *gatedConnPool) Ping() error {
	return p.db.Ping()
}

// GetDBConn satisfies gorm's GetDBConnector so db.DB() (pool sizing, stats)
// still resolves the real *sql.DB through the wrapper.
func (p *gatedConnPool) GetDBConn() (*sql.DB, error) {
	return p.db, nil
}

// BeginTx implements gorm.ConnPoolBeginner: gorm's Begin() routes every
// transaction — explicit Transaction(...) calls and the implicit one wrapping
// each Create/Update/Delete — through here.
//
// A handler already running inside a subscriber's batch transaction (pericarp
// attaches it via HandlerContext; detected with TxFromContext) bypasses the
// gate: the batch's own BeginTx acquired it higher up this goroutine's call
// stack, and re-acquiring a non-reentrant semaphore would deadlock. The
// bypassed Begin still contends with the batch inside SQLite itself, bounded
// by busy_timeout — the bypass's only job is to keep that contention from
// becoming a permanent gate deadlock. The root fix (pericarp's Append joining
// the ambient transaction) ships separately in akeemphilbert/pericarp#58 and
// takes effect once weos pins a release containing it.
func (p *gatedConnPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	if subscriptions.TxFromContext(ctx) != nil {
		tx, err := p.db.BeginTx(ctx, opts)
		if err != nil {
			return nil, err
		}
		return &gatedTx{tx: tx}, nil
	}
	if err := p.gate.Lock(ctx); err != nil {
		return nil, err
	}
	tx, err := p.beginWithBusyRetry(ctx, opts)
	if err != nil {
		p.gate.Unlock()
		return nil, err
	}
	gt := &gatedTx{tx: tx, gate: p.gate}
	runtime.SetFinalizer(gt, finalizeLeakedGatedTx)
	return gt, nil
}

// beginWithBusyRetry retries BeginTx on SQLITE_BUSY with doubling backoff,
// bounded at busyRetryMaxAttempts. Any other error surfaces on the first
// attempt. The gate is already held, so BUSY here means contention the gate
// cannot see: another process on the file, or a second in-process pool.
func (p *gatedConnPool) beginWithBusyRetry(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	delay := busyRetryInitialDelay
	var lastErr error
	for attempt := 1; attempt <= busyRetryMaxAttempts; attempt++ {
		tx, err := p.db.BeginTx(ctx, opts)
		notifyBusyObserver(attempt, err)
		if err == nil {
			return tx, nil
		}
		if !isSQLiteBusy(err) {
			return nil, err
		}
		lastErr = err
		if attempt == busyRetryMaxAttempts {
			break
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			// Keep the BUSY that drove us into backoff visible — a bare
			// ctx.Err() is indistinguishable from a slow query.
			return nil, fmt.Errorf(
				"begin sqlite write transaction: %w (interrupted after %d attempts; last SQLITE_BUSY: %v)",
				ctx.Err(), attempt, lastErr)
		}
		delay *= 2
		if delay > busyRetryMaxDelay {
			delay = busyRetryMaxDelay
		}
	}
	return nil, fmt.Errorf(
		"begin sqlite write transaction: gave up after %d attempts: %w",
		busyRetryMaxAttempts, lastErr)
}

// gatedTx wraps the write transaction and releases the gate exactly once, on
// whichever of Commit/Rollback runs first. A nil gate means the transaction
// was begun via the ambient-batch bypass and holds nothing.
//
// A wrapper that becomes unreachable without either being called would hold
// the gate forever — a silent, process-wide write wedge (strictly worse than
// the pre-gate behavior, where a leaked transaction surfaced as loud BUSY
// errors and self-healed at busy_timeout). BeginTx therefore arms a finalizer
// as a loud last resort: it rolls the inner transaction back, releases the
// gate, and reports the leak on stderr.
type gatedTx struct {
	tx       *sql.Tx
	gate     *writeGate
	released atomic.Bool
}

func (t *gatedTx) release() {
	if t.gate != nil && t.released.CompareAndSwap(false, true) {
		t.gate.Unlock()
	}
}

// finalizeLeakedGatedTx is the last-resort release for a gated transaction
// that was dropped without Commit/Rollback (e.g. a panic between gorm's begin
// and commit callbacks, recovered upstream). Rollback on an already-finished
// inner transaction returns sql.ErrTxDone harmlessly.
func finalizeLeakedGatedTx(t *gatedTx) {
	if t.gate == nil || t.released.Load() {
		return
	}
	fmt.Fprintln(os.Stderr,
		"weos: sqlite write gate: transaction leaked without Commit/Rollback; rolling back and releasing the gate")
	_ = t.tx.Rollback()
	t.release()
}

func (t *gatedTx) Commit() error {
	defer t.release()
	return t.tx.Commit()
}

func (t *gatedTx) Rollback() error {
	defer t.release()
	return t.tx.Rollback()
}

func (t *gatedTx) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return t.tx.PrepareContext(ctx, query)
}

func (t *gatedTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

func (t *gatedTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

func (t *gatedTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}

func (t *gatedTx) StmtContext(ctx context.Context, stmt *sql.Stmt) *sql.Stmt {
	return t.tx.StmtContext(ctx, stmt)
}
