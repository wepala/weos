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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	gormlib "gorm.io/gorm"
)

// The lag query names pericarp's tables directly; if pericarp renames one,
// this test fails before the query does.
func TestLexicalLagQuery_NamesPericarpsTables(t *testing.T) {
	t.Parallel()
	for _, table := range []string{
		infrastructure.GormEventModel{}.TableName(), subscriptions.GormCheckpointModel{}.TableName(),
	} {
		if !strings.Contains(lexicalLagQuery, "FROM "+table+" ") &&
			!strings.Contains(lexicalLagQuery, "FROM "+table+")") {
			t.Errorf("lexicalLagQuery does not read %q: %s", table, lexicalLagQuery)
		}
	}
}

// appendEvents stores n events through pericarp's store, which assigns their
// positions, and returns the newest position.
func appendEvents(t *testing.T, db *gormlib.DB, n int) int64 {
	t.Helper()
	store, err := infrastructure.NewGormEventStore(db)
	if err != nil {
		t.Fatalf("event store: %v", err)
	}
	ctx := context.Background()
	for i := 1; i <= n; i++ {
		agg := fmt.Sprintf("urn:task:%d", i)
		ev := domain.EventEnvelope[any]{
			ID: fmt.Sprintf("ev-%d", i), AggregateID: agg, EventType: "Resource.Created",
			Payload: map[string]any{"n": i}, SequenceNo: 1, Created: time.Now(),
		}
		if err := store.Append(ctx, agg, 0, ev); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	head, err := store.HeadPosition(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	return head
}

func setLexicalCheckpoint(t *testing.T, db *gormlib.DB, position int64) {
	t.Helper()
	row := subscriptions.GormCheckpointModel{
		Subscriber: repositories.LexicalIndexSubscriber, Position: position, UpdatedAt: time.Now(),
	}
	if err := db.Save(&row).Error; err != nil {
		t.Fatalf("set checkpoint: %v", err)
	}
}

func assertLag(t *testing.T, idx repositories.LexicalIndex, label string, want int64) {
	t.Helper()
	got, err := idx.Lag(context.Background())
	if err != nil || got != want {
		t.Errorf("%s: Lag = %d, %v; want %d, nil", label, got, err, want)
	}
}

// runLagCases: the lag is the newest event position less the lexical-index
// checkpoint — the whole history when the subscriber has never run, 0 once it
// has caught up, and never negative.
func runLagCases(t *testing.T, idx repositories.LexicalIndex, db *gormlib.DB) {
	t.Helper()
	if _, err := infrastructure.NewGormEventStore(db); err != nil { // creates the events table
		t.Fatalf("event store: %v", err)
	}
	assertLag(t, idx, "empty store, no checkpoint", 0)
	head := appendEvents(t, db, 5)
	assertLag(t, idx, "no checkpoint yet", head)
	setLexicalCheckpoint(t, db, head-3)
	assertLag(t, idx, "part refilled", 3)
	setLexicalCheckpoint(t, db, head)
	assertLag(t, idx, "caught up", 0)
	setLexicalCheckpoint(t, db, head+10)
	assertLag(t, idx, "checkpoint past the newest surviving event", 0)
}

func TestLexicalIndex_Lag_SQLite(t *testing.T) {
	t.Parallel()
	db := sqliteScopeDB(t)
	runLagCases(t, ProvideLexicalIndex(db, config.Default(), &testLogger{}), db)
}

func TestLexicalIndex_Lag_Postgres(t *testing.T) {
	t.Parallel()
	db, cfg := postgresScopeDB(t)
	runLagCases(t, ProvideLexicalIndex(db, cfg, &testLogger{}), db)
}
