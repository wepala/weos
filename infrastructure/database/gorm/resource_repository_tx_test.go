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

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
)

// TestResourceWrite_JoinsBatchTransaction pins the same contract for
// ResourceRepository that TestProjectionWrite_JoinsBatchTransaction pins for
// the projection manager: a resource write invoked from inside a subscriber
// handler must run through the batch transaction pericarp puts in the context
// (writeDB → subscriptions.TxFromContext), NOT through the pooled *gorm.DB.
// On SQLite a pooled-connection write blocks on the batch's own write lock
// until busy_timeout, then errors "database is locked" — a self-deadlock
// inside the subscriber goroutine. Reverting writeDB to r.db reintroduces
// exactly that; this test catches it.
func TestResourceWrite_JoinsBatchTransaction(t *testing.T) {
	db := newWALTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}); err != nil {
		t.Fatalf("auto-migrate resources: %v", err)
	}
	const id = "urn:widget:tx-guardrail"
	if err := db.Create(&models.Resource{ID: id, TypeSlug: "widget", Data: "{}"}).Error; err != nil {
		t.Fatalf("seed resource: %v", err)
	}

	repo := &ResourceRepository{
		db:      db,
		projMgr: &projectionManager{db: db, logger: &testLogger{}},
		logger:  &testLogger{},
	}

	cps, err := subscriptions.NewGormCheckpointStore(db)
	if err != nil {
		t.Fatalf("new checkpoint store: %v", err)
	}
	batch, acquired, err := cps.Acquire(context.Background(), "resource-guardrail")
	if err != nil || !acquired {
		t.Fatalf("acquire batch: acquired=%v err=%v", acquired, err)
	}
	defer func() { _ = batch.Rollback() }()
	handlerCtx := batch.HandlerContext(context.Background())

	// Must not deadlock: pre-fix (writing through r.db) this takes a second
	// pooled connection and blocks on the batch's write lock until
	// busy_timeout, then errors "database is locked".
	done := make(chan error, 1)
	go func() {
		done <- repo.Delete(handlerCtx, id)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resource write inside the batch transaction failed "+
				"(deadlock regression — write did not join the batch tx): %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("resource write blocked inside the batch transaction " +
			"(deadlock regression — write did not join the batch tx)")
	}

	// Routing proof: the delete lives in the still-open batch transaction, so
	// a pooled read must still see the row before commit.
	var preCommit int64
	if err := db.Model(&models.Resource{}).Where("id = ?", id).Count(&preCommit).Error; err != nil {
		t.Fatalf("pre-commit count: %v", err)
	}
	if preCommit != 1 {
		t.Fatalf("pre-commit pooled read found %d rows, want 1 (write leaked outside the batch tx)", preCommit)
	}

	if err := batch.Commit(context.Background(), 1); err != nil {
		t.Fatalf("commit batch: %v", err)
	}
	var postCommit int64
	if err := db.Model(&models.Resource{}).Where("id = ?", id).Count(&postCommit).Error; err != nil {
		t.Fatalf("post-commit count: %v", err)
	}
	if postCommit != 0 {
		t.Fatalf("post-commit read found %d rows, want 0", postCommit)
	}
}
