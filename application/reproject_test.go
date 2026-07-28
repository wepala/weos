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
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// reprojectTestEnv builds the REAL reproject runtime — the same narrow fx
// assembly `worker reproject` uses — against a scratch file-backed SQLite DB
// (file-backed, not :memory:, so every pooled connection sees the same
// database). Events are appended straight to the store, which is exactly the
// state a pericarp import produces: full history, empty projections, payloads
// deserializing as map[string]any.
type reprojectTestEnv struct {
	rt  ReprojectRuntime
	db  *gorm.DB
	app *fx.App
}

func newReprojectTestEnv(t testing.TB) *reprojectTestEnv {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "reproject_test.db") +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_txlock=immediate"

	env := &reprojectTestEnv{}
	env.app = fx.New(
		fx.NopLogger,
		ReprojectModule(config.Config{DatabaseDSN: dsn}),
		fx.Populate(&env.rt),
		fx.Populate(&env.db),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := env.app.Start(startCtx); err != nil {
		t.Fatalf("start reproject runtime: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = env.app.Stop(stopCtx)
	})
	return env
}

// seedImportedHistory writes the shape of a real twin's history for one
// resource type and one resource, using the exact payload structs the write
// path emits (the store serializes them; ReadAfter then yields
// map[string]any — the imported-store shape reproject must handle):
//
//	pos 1  ResourceType.Created  (type "note")
//	pos 2  Resource.Created      ┐
//	pos 3  Triple.Created        ├ one transaction (tx-1)
//	pos 4  Resource.Published    ┘
//	pos 5  Custom.Ignored        (no synchronous handler — must be skipped)
func (e *reprojectTestEnv) seedImportedHistory(t testing.TB) {
	t.Helper()
	ctx := context.Background()

	schema := json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`)
	ldCtx := json.RawMessage(`{"@vocab":"https://example.com/ns#"}`)

	rtCreated := domain.EventEnvelope[any]{
		ID:          "evt-rt-1",
		AggregateID: "urn:type:note",
		EventType:   "ResourceType.Created",
		Payload: entities.ResourceTypeCreated{
			Name: "Note", Slug: "note", Description: "a note",
			Context: ldCtx, Schema: schema, Timestamp: time.Now().UTC(),
		},
		Created:    time.Now().UTC(),
		SequenceNo: 1,
	}
	if err := e.rt.EventStore.Append(ctx, rtCreated.AggregateID, 0, rtCreated); err != nil {
		t.Fatalf("append ResourceType.Created: %v", err)
	}

	data := json.RawMessage(`{"@id":"urn:note:abc","@type":"note","title":"hello"}`)
	resourceEvents := []domain.EventEnvelope[any]{
		{
			ID:          "evt-r-1",
			AggregateID: "urn:note:abc",
			EventType:   "Resource.Created",
			Payload: map[string]any{
				"TypeSlug":  "note",
				"Data":      json.RawMessage(data),
				"CreatedBy": "urn:agent:test",
				"AccountID": "",
				"Timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			},
			Created:       time.Now().UTC(),
			SequenceNo:    1,
			TransactionID: "tx-1",
		},
		{
			ID:          "evt-t-1",
			AggregateID: "urn:note:abc",
			EventType:   "Triple.Created",
			Payload: entities.TripleCreated{}.With(
				"urn:note:abc", "relatesTo", "urn:note:def", "",
			),
			Created:       time.Now().UTC(),
			SequenceNo:    2,
			TransactionID: "tx-1",
		},
		{
			ID:          "evt-p-1",
			AggregateID: "urn:note:abc",
			EventType:   "Resource.Published",
			Payload:     entities.ResourcePublished{}.With("note", ""),
			Created:     time.Now().UTC(),
			SequenceNo:  3,

			TransactionID: "tx-1",
		},
	}
	for _, env := range resourceEvents {
		if err := e.rt.EventStore.Append(ctx, env.AggregateID, env.SequenceNo-1, env); err != nil {
			t.Fatalf("append %s: %v", env.EventType, err)
		}
	}

	ignored := domain.EventEnvelope[any]{
		ID:          "evt-x-1",
		AggregateID: "agg-ignored",
		EventType:   "Custom.Ignored",
		Payload:     map[string]any{"n": 1},
		Created:     time.Now().UTC(),
		SequenceNo:  1,
	}
	if err := e.rt.EventStore.Append(ctx, ignored.AggregateID, 0, ignored); err != nil {
		t.Fatalf("append Custom.Ignored: %v", err)
	}
}

func (e *reprojectTestEnv) count(t testing.TB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := e.db.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func TestReproject_MaterializesSynchronousProjections(t *testing.T) {
	env := newReprojectTestEnv(t)
	env.seedImportedHistory(t)

	// The imported-store premise: history present, projections empty.
	if n := env.count(t, `SELECT COUNT(*) FROM resource_types`); n != 0 {
		t.Fatalf("expected empty resource_types before reproject, got %d", n)
	}

	res, err := Reproject(context.Background(), env.rt, ReprojectOptions{})
	if err != nil {
		t.Fatalf("reproject: %v", err)
	}

	// ResourceType.Created + Triple.Created + Resource.Published are handled;
	// Resource.Created (folded in via the Published handler's transaction
	// read) and Custom.Ignored are not.
	if res.Dispatched != 3 {
		t.Errorf("dispatched = %d, want 3", res.Dispatched)
	}
	if res.Skipped != 2 {
		t.Errorf("skipped = %d, want 2", res.Skipped)
	}
	if res.LastPosition != res.Head {
		t.Errorf("last position %d != head %d", res.LastPosition, res.Head)
	}

	if n := env.count(t, `SELECT COUNT(*) FROM resource_types WHERE slug = 'note'`); n != 1 {
		t.Errorf("resource_types rows for 'note' = %d, want 1", n)
	}
	if n := env.count(t, `SELECT COUNT(*) FROM resources WHERE id = 'urn:note:abc'`); n != 1 {
		t.Errorf("resources rows for urn:note:abc = %d, want 1", n)
	}
	if n := env.count(t,
		`SELECT COUNT(*) FROM triples WHERE subject = 'urn:note:abc' AND predicate = 'relatesTo'`,
	); n != 1 {
		t.Errorf("triples rows = %d, want 1", n)
	}
}

func TestReproject_IsIdempotent(t *testing.T) {
	env := newReprojectTestEnv(t)
	env.seedImportedHistory(t)

	if _, err := Reproject(context.Background(), env.rt, ReprojectOptions{}); err != nil {
		t.Fatalf("first reproject: %v", err)
	}
	res2, err := Reproject(context.Background(), env.rt, ReprojectOptions{})
	if err != nil {
		t.Fatalf("second reproject: %v", err)
	}
	if res2.Dispatched != 3 {
		t.Errorf("second run dispatched = %d, want 3", res2.Dispatched)
	}

	if n := env.count(t, `SELECT COUNT(*) FROM resource_types WHERE slug = 'note'`); n != 1 {
		t.Errorf("resource_types rows after re-run = %d, want 1", n)
	}
	if n := env.count(t, `SELECT COUNT(*) FROM resources WHERE id = 'urn:note:abc'`); n != 1 {
		t.Errorf("resources rows after re-run = %d, want 1", n)
	}
}

// seedDeletedTypeHistory appends a type that was created and then deleted —
// the history shape that leaves a soft-deleted resource_types row behind
// after one replay pass.
func (e *reprojectTestEnv) seedDeletedTypeHistory(t testing.TB) {
	t.Helper()
	ctx := context.Background()

	events := []domain.EventEnvelope[any]{
		{
			ID:          "evt-rt-d1",
			AggregateID: "urn:type:draft",
			EventType:   "ResourceType.Created",
			Payload: entities.ResourceTypeCreated{
				Name: "Draft", Slug: "draft", Description: "a draft",
				Context:   json.RawMessage(`{"@vocab":"https://example.com/ns#"}`),
				Schema:    json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`),
				Timestamp: time.Now().UTC(),
			},
			Created:    time.Now().UTC(),
			SequenceNo: 1,
		},
		{
			ID:          "evt-rt-d2",
			AggregateID: "urn:type:draft",
			EventType:   "ResourceType.Deleted",
			Payload:     entities.ResourceTypeDeleted{}.With(),
			Created:     time.Now().UTC(),
			SequenceNo:  2,
		},
	}
	for _, env := range events {
		if err := e.rt.EventStore.Append(ctx, env.AggregateID, env.SequenceNo-1, env); err != nil {
			t.Fatalf("append %s: %v", env.EventType, err)
		}
	}
}

// A history that deletes a type leaves a soft-deleted row after the first
// pass. The Created handler's existence probe must see that row (FindByID
// filters deleted_at) or the second pass fails Save on the primary key.
func TestReproject_IsIdempotentOverDeletedTypeHistory(t *testing.T) {
	env := newReprojectTestEnv(t)
	env.seedDeletedTypeHistory(t)

	if _, err := Reproject(context.Background(), env.rt, ReprojectOptions{}); err != nil {
		t.Fatalf("first reproject: %v", err)
	}
	if _, err := Reproject(context.Background(), env.rt, ReprojectOptions{}); err != nil {
		t.Fatalf("second reproject over soft-deleted type: %v", err)
	}

	if n := env.count(t, `SELECT COUNT(*) FROM resource_types WHERE slug = 'draft'`); n != 1 {
		t.Errorf("resource_types rows for 'draft' = %d, want 1", n)
	}
	if n := env.count(t,
		`SELECT COUNT(*) FROM resource_types WHERE slug = 'draft' AND deleted_at IS NOT NULL`,
	); n != 1 {
		t.Errorf("expected the 'draft' row to converge soft-deleted after replay")
	}
}

func TestReproject_ResumesAfterPosition(t *testing.T) {
	env := newReprojectTestEnv(t)
	env.seedImportedHistory(t)

	head, err := env.rt.EventStore.HeadPosition(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	res, err := Reproject(context.Background(), env.rt, ReprojectOptions{AfterPosition: head})
	if err != nil {
		t.Fatalf("reproject: %v", err)
	}
	if res.Dispatched != 0 || res.Skipped != 0 {
		t.Errorf("resume at head replayed %d/%d events, want 0/0", res.Dispatched, res.Skipped)
	}
}

func TestTypedForReplay_ConvertsStoreShapedPayloads(t *testing.T) {
	cases := []struct {
		eventType string
		payload   map[string]any
		want      any
	}{
		{
			// Untagged struct: store maps carry PascalCase keys.
			eventType: "ResourceType.Deleted",
			payload:   map[string]any{"Timestamp": "2026-07-28T00:00:00Z"},
			want:      entities.ResourceTypeDeleted{},
		},
		{
			// Tagged embed (BasicTripleEvent): store maps carry json-tag keys.
			eventType: "Triple.Created",
			payload:   map[string]any{"subject": "urn:a:1", "predicate": "p", "object": "urn:b:2"},
			want:      entities.TripleCreated{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.eventType, func(t *testing.T) {
			env := domain.EventEnvelope[any]{EventType: tc.eventType, Payload: tc.payload}
			typed, handled, err := typedForReplay(env)
			if err != nil {
				t.Fatalf("typedForReplay: %v", err)
			}
			if !handled {
				t.Fatal("expected event to be handled")
			}
			if fmt.Sprintf("%T", typed.Payload) != fmt.Sprintf("%T", tc.want) {
				t.Fatalf("payload type = %T, want %T", typed.Payload, tc.want)
			}
			if tr, ok := typed.Payload.(entities.TripleCreated); ok && tr.Subject != "urn:a:1" {
				t.Errorf("triple subject = %q, want urn:a:1", tr.Subject)
			}
		})
	}

	env := domain.EventEnvelope[any]{EventType: "Custom.Ignored", Payload: map[string]any{"n": 1}}
	if _, handled, err := typedForReplay(env); handled || err != nil {
		t.Fatalf("unhandled event: handled=%v err=%v, want false/nil", handled, err)
	}
}
