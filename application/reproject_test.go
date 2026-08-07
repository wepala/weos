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

// seedSchemaChangeHistory writes the history shape issue #379 is about: a type
// created WITHOUT a property, resources written carrying that property anyway,
// and only then a ResourceType.Updated adding it. Replaying this in one strict
// position order projects the resources against the old column set and loses
// the value; the two-pass order is what recovers it.
//
//	pos 1  ResourceType.Created   (note, schema {title})
//	pos 2  ResourceType.Unarchived (no synchronous handler — a ResourceType.*
//	                                event pass 1 must still count as skipped)
//	pos 3  Resource.Created       ┐
//	pos 4  Triple.Created         ├ one transaction (tx-1), data carries `sku`
//	pos 5  Resource.Published     ┘
//	pos 6  ResourceType.Updated   (note, schema {title, sku}) — LAST in the feed
func (e *reprojectTestEnv) seedSchemaChangeHistory(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	ldCtx := json.RawMessage(`{"@vocab":"https://example.com/ns#"}`)
	oldSchema := json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`)
	newSchema := json.RawMessage(
		`{"type":"object","properties":{"title":{"type":"string"},"sku":{"type":"string"}}}`)

	appendEvt := func(aggregateID, eventType string, payload any, seq int, txID string) {
		t.Helper()
		env := domain.EventEnvelope[any]{
			ID:            fmt.Sprintf("evt-%s-%d", eventType, seq),
			AggregateID:   aggregateID,
			EventType:     eventType,
			Payload:       payload,
			Created:       time.Now().UTC(),
			SequenceNo:    seq,
			TransactionID: txID,
		}
		if err := e.rt.EventStore.Append(ctx, aggregateID, seq-1, env); err != nil {
			t.Fatalf("append %s: %v", eventType, err)
		}
	}

	appendEvt("urn:type:note", "ResourceType.Created", entities.ResourceTypeCreated{
		Name: "Note", Slug: "note", Description: "a note",
		Context: ldCtx, Schema: oldSchema, Timestamp: time.Now().UTC(),
	}, 1, "")
	// A ResourceType.* event typedForReplay does not handle. Pass 2's accept
	// filter excludes it, so only pass 1 can account for it.
	appendEvt("urn:type:note", "ResourceType.Unarchived", map[string]any{"n": 1}, 2, "")

	data := json.RawMessage(`{"@id":"urn:note:abc","@type":"note","title":"hello","sku":"BC-100"}`)
	appendEvt("urn:note:abc", "Resource.Created", map[string]any{
		"TypeSlug": "note", "Data": data, "CreatedBy": "urn:agent:test", "AccountID": "",
		"Timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}, 1, "tx-1")
	appendEvt("urn:note:abc", "Triple.Created",
		entities.TripleCreated{}.With("urn:note:abc", "relatesTo", "urn:note:def", ""), 2, "tx-1")
	appendEvt("urn:note:abc", "Resource.Published",
		entities.ResourcePublished{}.With("note", ""), 3, "tx-1")

	appendEvt("urn:type:note", "ResourceType.Updated", entities.ResourceTypeUpdated{
		Name: "Note", Slug: "note", Description: "a note", Status: "active",
		Context: ldCtx, Schema: newSchema, Timestamp: time.Now().UTC(),
	}, 3, "")
}

// TestReproject_BackfillsColumnAddedAfterTheRows is the unit-level guard for the
// two-pass order. Replaying in one position order applies the type's ORIGINAL
// schema, projects the resources against it, and only then the update that adds
// `sku` — so the column stays NULL. Draining type events first recovers it.
func TestReproject_BackfillsColumnAddedAfterTheRows(t *testing.T) {
	env := newReprojectTestEnv(t)
	env.seedSchemaChangeHistory(t)

	if _, err := Reproject(context.Background(), env.rt, ReprojectOptions{}); err != nil {
		t.Fatalf("reproject: %v", err)
	}
	if n := env.count(t,
		`SELECT COUNT(*) FROM notes WHERE id = 'urn:note:abc' AND sku = 'BC-100'`,
	); n != 1 {
		t.Errorf("sku was not backfilled onto the projection row (matching rows = %d)", n)
	}
}

// TestReproject_ResumeStillRunsTypePass pins that pass 1 ignores AfterPosition.
// Resuming past the type events must STILL bring the type to its final shape,
// or the resources replaying in pass 2 project against a column set that was
// never established.
func TestReproject_ResumeStillRunsTypePass(t *testing.T) {
	env := newReprojectTestEnv(t)
	env.seedSchemaChangeHistory(t)

	// Resume from position 2 — past both type events that precede the
	// resources. Only pass 1 replaying from the start of the feed can give the
	// projection its table and its `sku` column.
	res, err := Reproject(context.Background(), env.rt, ReprojectOptions{AfterPosition: 2})
	if err != nil {
		t.Fatalf("resumed reproject: %v", err)
	}
	if res.LastPosition != res.Head {
		t.Errorf("resume LastPosition = %d, want head %d", res.LastPosition, res.Head)
	}
	if n := env.count(t,
		`SELECT COUNT(*) FROM notes WHERE id = 'urn:note:abc' AND sku = 'BC-100'`,
	); n != 1 {
		t.Errorf("a resume did not run the type pass: sku missing (matching rows = %d)", n)
	}
}

// TestReproject_CompleteRunReachesHead pins the number an operator feeds back as
// --after-position. Pass 2 only advances LastPosition on events it accepts, and
// this feed ends with a ResourceType.Updated, so without the terminal assignment
// a fully successful run under-reports.
func TestReproject_CompleteRunReachesHead(t *testing.T) {
	env := newReprojectTestEnv(t)
	env.seedSchemaChangeHistory(t)

	res, err := Reproject(context.Background(), env.rt, ReprojectOptions{})
	if err != nil {
		t.Fatalf("reproject: %v", err)
	}
	if res.LastPosition != res.Head {
		t.Errorf("complete run reported LastPosition %d, want head %d", res.LastPosition, res.Head)
	}
}

// TestReproject_CountsEachEventOnce guards the two-pass split. The feed carries
// a ResourceType.* event with no synchronous handler, which only pass 1 can
// account for — drop countSkip there and it falls out of both counters.
func TestReproject_CountsEachEventOnce(t *testing.T) {
	env := newReprojectTestEnv(t)
	env.seedSchemaChangeHistory(t)

	res, err := Reproject(context.Background(), env.rt, ReprojectOptions{})
	if err != nil {
		t.Fatalf("reproject: %v", err)
	}
	// 6 events: ResourceType.Created, ResourceType.Unarchived, Resource.Created,
	// Triple.Created, Resource.Published, ResourceType.Updated. Two lack a
	// synchronous handler (Unarchived; Created, folded into Published).
	if got := res.Dispatched + res.Skipped; got != 6 {
		t.Errorf("Dispatched+Skipped = %d, want 6 (dispatched=%d skipped=%d)",
			got, res.Dispatched, res.Skipped)
	}
	if res.Skipped != 2 {
		t.Errorf("Skipped = %d, want exactly 2", res.Skipped)
	}
}
