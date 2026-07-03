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
	"testing"
	"time"

	"github.com/wepala/weos/v3/infrastructure/models"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"gorm.io/gorm"
)

// TestLexicalIndex_JoinsBatchTransaction is the regression guardrail for the
// SQLite self-deadlock fixed on the epic #409 branch: the lexical group's
// Index/Remove writes ran on a pooled connection while the subscriber's batch
// transaction held the write lock (_txlock=immediate), blocking until
// busy_timeout and then retrying inside the still-open batch. The writes must
// join the batch transaction via subscriptions.TxFromContext. Same shape as
// TestProjectionWrite_JoinsBatchTransaction, which guards the projection
// manager against the identical bug class.
func TestLexicalIndex_JoinsBatchTransaction(t *testing.T) {
	db := newWALTestDB(t)
	idx := ProvideLexicalIndex(db, config.Default(), &testLogger{})
	if !idx.Active() {
		t.Skip("FTS5 unavailable; lexical index inactive")
	}

	cps, err := subscriptions.NewGormCheckpointStore(db)
	if err != nil {
		t.Fatalf("new checkpoint store: %v", err)
	}
	batch, acquired, err := cps.Acquire(context.Background(), "lexical-guardrail")
	if err != nil || !acquired {
		t.Fatalf("acquire batch: acquired=%v err=%v", acquired, err)
	}
	defer func() { _ = batch.Rollback() }()
	handlerCtx := batch.HandlerContext(context.Background())

	// Index (nested Transaction → savepoint under the batch tx) and Remove
	// must both complete without deadlocking against the batch's write lock.
	done := make(chan error, 1)
	go func() {
		if err := idx.Index(handlerCtx, "urn:task:g1", "task", "guardrail content"); err != nil {
			done <- err
			return
		}
		done <- idx.Remove(handlerCtx, "urn:task:gone")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("lexical write inside the batch transaction failed "+
				"(deadlock regression — write did not join the batch tx): %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("lexical write blocked inside the batch transaction " +
			"(deadlock regression — write did not join the batch tx)")
	}

	// Pre-commit, a pooled read must not see the uncommitted index row.
	if got := countSearchRows(t, db); got != 0 {
		t.Fatalf("pre-commit pooled read sees %d rows, want 0 (write leaked outside the batch tx)", got)
	}
	if err := batch.Commit(context.Background(), 1); err != nil {
		t.Fatalf("commit batch: %v", err)
	}
	if got := countSearchRows(t, db); got != 1 {
		t.Fatalf("post-commit read sees %d rows, want 1", got)
	}
}

// TestEventReferenceSave_JoinsBatchTransaction pins the same guarantee for the
// event-reference projection's SaveForEvent.
func TestEventReferenceSave_JoinsBatchTransaction(t *testing.T) {
	db := newWALTestDB(t)
	if err := db.AutoMigrate(&models.EventReference{}); err != nil {
		t.Fatalf("migrate event_references: %v", err)
	}
	repo := &EventReferenceRepository{db: db}

	cps, err := subscriptions.NewGormCheckpointStore(db)
	if err != nil {
		t.Fatalf("new checkpoint store: %v", err)
	}
	batch, acquired, err := cps.Acquire(context.Background(), "event-refs-guardrail")
	if err != nil || !acquired {
		t.Fatalf("acquire batch: acquired=%v err=%v", acquired, err)
	}
	defer func() { _ = batch.Rollback() }()
	handlerCtx := batch.HandlerContext(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- repo.SaveForEvent(handlerCtx, "ev1", []string{"urn:task:g1"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reference write inside the batch transaction failed "+
				"(deadlock regression — write did not join the batch tx): %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reference write blocked inside the batch transaction " +
			"(deadlock regression — write did not join the batch tx)")
	}

	refs, err := repo.ForEvents(context.Background(), []string{"ev1"})
	if err != nil {
		t.Fatalf("ForEvents: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("pre-commit pooled read sees refs %v, want none (write leaked outside the batch tx)", refs)
	}
	if err := batch.Commit(context.Background(), 1); err != nil {
		t.Fatalf("commit batch: %v", err)
	}
	refs, err = repo.ForEvents(context.Background(), []string{"ev1"})
	if err != nil {
		t.Fatalf("ForEvents post-commit: %v", err)
	}
	if len(refs["ev1"]) != 1 {
		t.Fatalf("post-commit refs = %v, want the saved reference", refs)
	}
}

// countSearchRows counts resource_search rows through the pooled connection
// (deliberately not the batch transaction).
func countSearchRows(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var count int
	if err := db.Raw(`SELECT COUNT(*) FROM resource_search`).Row().Scan(&count); err != nil {
		t.Fatalf("count resource_search rows: %v", err)
	}
	return count
}
