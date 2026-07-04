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
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type gateTestRow struct {
	ID   uint `gorm:"primarykey"`
	Name string
}

func openGatedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "gate_test.db")
	db, err := gorm.Open(DialectorForDSN(dsn), gormConfig())
	if err != nil {
		t.Fatalf("open gated db: %v", err)
	}
	if err := db.AutoMigrate(&gateTestRow{}); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}
	return db
}

// TestWriteGate_MutualExclusion proves at most one holder at a time and that
// waiters proceed after release.
func TestWriteGate_MutualExclusion(t *testing.T) {
	g := newWriteGate()
	var inGate, maxSeen int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := g.Lock(context.Background()); err != nil {
				t.Errorf("lock: %v", err)
				return
			}
			n := atomic.AddInt32(&inGate, 1)
			for {
				m := atomic.LoadInt32(&maxSeen)
				if n <= m || atomic.CompareAndSwapInt32(&maxSeen, m, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&inGate, -1)
			g.Unlock()
		})
	}
	wg.Wait()
	if got := atomic.LoadInt32(&maxSeen); got != 1 {
		t.Fatalf("expected at most 1 concurrent holder, saw %d", got)
	}
}

func TestWriteGate_LockHonorsContext(t *testing.T) {
	g := newWriteGate()
	if err := g.Lock(context.Background()); err != nil {
		t.Fatalf("initial lock: %v", err)
	}
	defer g.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := g.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded while queued, got %v", err)
	}
}

// TestGatedDB_ConcurrentWriteBurst is the in-package regression for the #421
// failure: many goroutines writing transactionally at once must all land with
// zero "database is locked" errors.
func TestGatedDB_ConcurrentWriteBurst(t *testing.T) {
	db := openGatedTestDB(t)

	const writers = 20
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			errs <- db.Transaction(func(tx *gorm.DB) error {
				return tx.Create(&gateTestRow{Name: fmt.Sprintf("row-%d", i)}).Error
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}

	var count int64
	if err := db.Model(&gateTestRow{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != writers {
		t.Fatalf("expected %d rows, got %d", writers, count)
	}
}

// TestGatedDB_ReadsPassWhileWriteHeld: a SELECT must complete while a write
// transaction holds the gate — reads never queue on it.
func TestGatedDB_ReadsPassWhileWriteHeld(t *testing.T) {
	db := openGatedTestDB(t)
	if err := db.Create(&gateTestRow{Name: "seed"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	if err := tx.Create(&gateTestRow{Name: "in-flight"}).Error; err != nil {
		t.Fatalf("create in tx: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		var rows []gateTestRow
		done <- db.Find(&rows).Error
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read during held write: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read blocked behind the write gate")
	}

	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestGatedDB_SecondWriterQueuesUntilCommit: a second write transaction waits
// for the first to finish instead of failing with SQLITE_BUSY.
func TestGatedDB_SecondWriterQueuesUntilCommit(t *testing.T) {
	db := openGatedTestDB(t)

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}

	second := make(chan error, 1)
	go func() {
		second <- db.Transaction(func(inner *gorm.DB) error {
			return inner.Create(&gateTestRow{Name: "queued"}).Error
		})
	}()

	select {
	case err := <-second:
		t.Fatalf("second writer finished while gate was held (err=%v)", err)
	case <-time.After(200 * time.Millisecond):
		// still queued — expected
	}

	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit: %v", err)
	}
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("queued writer failed after release: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued writer never proceeded after commit")
	}
}

// TestIsSQLiteBusy_RealDriverError manufactures genuine BUSY contention with
// two raw connections (the driver's error fields are unexported, so a real
// collision is the only honest way to produce one).
func TestIsSQLiteBusy_RealDriverError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy_test.db")
	holder, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer holder.Close()
	contender, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(0)&_txlock=immediate")
	if err != nil {
		t.Fatalf("open contender: %v", err)
	}
	defer contender.Close()

	if _, err := holder.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	holdTx, err := holder.Begin()
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer func() { _ = holdTx.Rollback() }()

	_, busyErr := contender.Begin()
	if busyErr == nil {
		t.Fatal("expected the contender's BEGIN IMMEDIATE to fail while the lock is held")
	}
	if !isSQLiteBusy(busyErr) {
		t.Fatalf("isSQLiteBusy(%v) = false, want true", busyErr)
	}
	if isSQLiteBusy(errors.New("some other failure")) {
		t.Fatal("isSQLiteBusy misclassified a generic error")
	}
}

// TestBeginWithBusyRetry_BoundedGiveUp holds the file lock past the whole
// retry budget and asserts the gate gives up after exactly
// busyRetryMaxAttempts attempts with a wrapped BUSY error.
func TestBeginWithBusyRetry_BoundedGiveUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "giveup_test.db")
	holder, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer holder.Close()
	if _, err := holder.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	pooled, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(0)&_txlock=immediate")
	if err != nil {
		t.Fatalf("open pooled: %v", err)
	}
	defer pooled.Close()
	pool := &gatedConnPool{db: pooled, gate: newWriteGate()}

	var attempts int32
	SetBusyRetryObserver(func(attempt int, err error) {
		atomic.AddInt32(&attempts, 1)
	})
	defer SetBusyRetryObserver(nil)

	holdTx, err := holder.Begin()
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer func() { _ = holdTx.Rollback() }()

	_, beginErr := pool.BeginTx(context.Background(), nil)
	if beginErr == nil {
		t.Fatal("expected BeginTx to give up while the lock is held")
	}
	wantMsg := fmt.Sprintf("gave up after %d attempts", busyRetryMaxAttempts)
	if !strings.Contains(beginErr.Error(), wantMsg) {
		t.Fatalf("error %q does not contain %q", beginErr.Error(), wantMsg)
	}
	if got := atomic.LoadInt32(&attempts); got != busyRetryMaxAttempts {
		t.Fatalf("expected %d attempts, observed %d", busyRetryMaxAttempts, got)
	}
}

// TestBeginWithBusyRetry_TransientBusyRecovers releases the external lock
// partway through the retry budget and asserts the write lands after more
// than one attempt.
func TestBeginWithBusyRetry_TransientBusyRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transient_test.db")
	holder, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer holder.Close()
	if _, err := holder.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	pooled, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(0)&_txlock=immediate")
	if err != nil {
		t.Fatalf("open pooled: %v", err)
	}
	defer pooled.Close()
	pool := &gatedConnPool{db: pooled, gate: newWriteGate()}

	var attempts int32
	SetBusyRetryObserver(func(attempt int, err error) {
		atomic.AddInt32(&attempts, 1)
	})
	defer SetBusyRetryObserver(nil)

	holdTx, err := holder.Begin()
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	go func() {
		time.Sleep(120 * time.Millisecond) // past attempt 1 (+50ms) and into attempt 2's window
		_ = holdTx.Rollback()
	}()

	tx, beginErr := pool.BeginTx(context.Background(), nil)
	if beginErr != nil {
		t.Fatalf("expected the write to land once the lock released, got %v", beginErr)
	}
	committer, ok := tx.(interface{ Rollback() error })
	if !ok {
		t.Fatalf("returned tx %T does not support Rollback", tx)
	}
	_ = committer.Rollback()

	if got := atomic.LoadInt32(&attempts); got < 2 {
		t.Fatalf("expected more than one attempt, observed %d", got)
	}
}

// TestGatedDialector_MemoryDSNSkipsGate: in-memory databases keep the plain
// dialector (a shared ":memory:" gate would serialize unrelated databases);
// file DSNs get the concrete glebarez dialector with a gated Conn preset —
// concrete so gorm still sees SavePoint/RollbackTo/Translate.
func TestGatedDialector_MemoryDSNSkipsGate(t *testing.T) {
	if d, ok := DialectorForDSN(":memory:").(*sqlite.Dialector); ok && d.Conn != nil {
		t.Fatal(":memory: DSN should not be write-gated")
	}
	d, ok := DialectorForDSN("file.db").(*sqlite.Dialector)
	if !ok {
		t.Fatalf("file DSN should produce the concrete glebarez dialector, got %T", DialectorForDSN("file.db"))
	}
	if _, ok := d.Conn.(*gatedConnPool); !ok {
		t.Fatalf("file DSN should be write-gated, Conn is %T", d.Conn)
	}
	if _, ok := DialectorForDSN("file.db").(gorm.SavePointerDialectorInterface); !ok {
		t.Fatal("gated dialector must keep SavePoint/RollbackTo visible (subscriber batches depend on savepoints)")
	}
}

// TestGatedDB_SavepointsWork pins the regression the interface-embedding
// wrapper caused: pericarp brackets every subscriber handler attempt in a
// savepoint, so SAVEPOINT/ROLLBACK TO must work on a gated transaction.
func TestGatedDB_SavepointsWork(t *testing.T) {
	db := openGatedTestDB(t)

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	if err := tx.SavePoint("sp1").Error; err != nil {
		t.Fatalf("savepoint on a gated tx: %v", err)
	}
	if err := tx.Create(&gateTestRow{Name: "discarded"}).Error; err != nil {
		t.Fatalf("create after savepoint: %v", err)
	}
	if err := tx.RollbackTo("sp1").Error; err != nil {
		t.Fatalf("rollback to savepoint: %v", err)
	}
	if err := tx.Create(&gateTestRow{Name: "kept"}).Error; err != nil {
		t.Fatalf("create after rollback-to: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit: %v", err)
	}

	var names []string
	if err := db.Model(&gateTestRow{}).Pluck("name", &names).Error; err != nil {
		t.Fatalf("pluck: %v", err)
	}
	if len(names) != 1 || names[0] != "kept" {
		t.Fatalf("expected only the post-savepoint row to survive, got %v", names)
	}
}

// TestWriteGateFor_SharedPerFile: same file (different DSN spellings) shares
// one gate; different files get independent gates.
func TestWriteGateFor_SharedPerFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "one.db")
	if writeGateFor(a) != writeGateFor("file:"+a+"?_pragma=busy_timeout(100)") {
		t.Fatal("same file should share one gate")
	}
	if writeGateFor(a) == writeGateFor(filepath.Join(dir, "two.db")) {
		t.Fatal("different files should have independent gates")
	}
}
