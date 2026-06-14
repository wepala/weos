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
	"path/filepath"
	"testing"
	"time"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestProjectionWrite_JoinsBatchTransaction is a regression guardrail for the
// SQLite projection deadlock that wedged statement imports.
//
// A projection write invoked from inside a subscriber handler must run through
// the batch transaction pericarp puts in the context (Batch.HandlerContext →
// subscriptions.TxFromContext), NOT through the pooled *gorm.DB. On SQLite a
// pooled-connection write takes a second connection and blocks on the batch's
// own write lock until _busy_timeout, returning "database is locked" — a
// self-deadlock inside the subscriber goroutine. projectionManager.writeDB
// closes that by joining the context transaction when one is present.
//
// The test uses a real file-backed WAL database with a multi-connection pool
// (the production shape). A single shared :memory: connection cannot reproduce
// the contention, so it would silently pass even with the bug reintroduced.
func TestProjectionWrite_JoinsBatchTransaction(t *testing.T) {
	db := newWALTestDB(t)

	if err := db.Exec(
		`CREATE TABLE widgets (id TEXT PRIMARY KEY, owner_id TEXT, owner_name TEXT)`,
	).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO widgets (id, owner_id, owner_name) VALUES ('w1', 'o1', 'old')`,
	).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}

	pm := &projectionManager{db: db, logger: &testLogger{}}
	pm.tables.Store("widget", tableInfo{
		name:    "widgets",
		columns: map[string]bool{"id": true, "owner_id": true, "owner_name": true},
	})

	// A pericarp batch holds an open write transaction (BEGIN IMMEDIATE under
	// _txlock=immediate) — exactly the lock the subscriber holds while a
	// handler runs.
	cps, err := subscriptions.NewGormCheckpointStore(db)
	if err != nil {
		t.Fatalf("new checkpoint store: %v", err)
	}
	batch, acquired, err := cps.Acquire(context.Background(), "guardrail")
	if err != nil || !acquired {
		t.Fatalf("acquire batch: acquired=%v err=%v", acquired, err)
	}
	defer func() { _ = batch.Rollback() }()

	handlerCtx := batch.HandlerContext(context.Background())

	// The core assertion: this must NOT deadlock. Pre-fix (writing through
	// pm.db) it takes a second pooled connection and blocks on the batch's
	// write lock until _busy_timeout, then errors "database is locked". Run it
	// off-goroutine so a hang (rather than an error) is also caught.
	done := make(chan error, 1)
	go func() {
		done <- pm.UpdateColumnByFK(handlerCtx, "widget", "owner_id", "o1", "owner_name", "new")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("projection write inside the batch transaction failed "+
				"(deadlock regression — write did not join the batch tx): %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("projection write blocked inside the batch transaction " +
			"(deadlock regression — write did not join the batch tx)")
	}

	// Routing proof: the write lives in the still-open batch transaction, so a
	// read on a separate pooled connection must still see the old value. Had
	// the write gone through pm.db it would either have deadlocked (above) or
	// already be visible here.
	if got := readOwnerName(t, db); got != "old" {
		t.Fatalf("pre-commit pooled read = %q, want \"old\" "+
			"(write leaked outside the batch transaction)", got)
	}

	if err := batch.Commit(context.Background(), 1); err != nil {
		t.Fatalf("commit batch: %v", err)
	}

	// After commit the batch transaction's write is visible everywhere.
	if got := readOwnerName(t, db); got != "new" {
		t.Fatalf("post-commit read = %q, want \"new\"", got)
	}
}

// readOwnerName reads widgets.owner_name for the seed row through the pooled
// connection (deliberately not the batch transaction).
func readOwnerName(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var name string
	if err := db.Raw(`SELECT owner_name FROM widgets WHERE id = ?`, "w1").Row().Scan(&name); err != nil {
		t.Fatalf("read owner_name: %v", err)
	}
	return name
}

// newWALTestDB opens a real file-backed SQLite database with the same worker
// pragmas the provider applies and a multi-connection pool, so batch-vs-pool
// write contention is reproducible. A single shared :memory: connection cannot
// deadlock against itself, so it would mask the bug this test guards against.
func newWALTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "guardrail.db") +
		"?_journal_mode=WAL&_busy_timeout=2000&_txlock=immediate"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open wal db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(5)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
