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
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// An ownerless resource: created by an authenticated agent whose session
// carried no account, so its Resource.Created stored an empty AccountID.
func ownerlessHistory(aggregateID, agent string) []domain.EventEnvelope[any] {
	return []domain.EventEnvelope[any]{
		{
			ID: aggregateID + "-created", EventType: "Resource.Created", AggregateID: aggregateID,
			SequenceNo: 1, TransactionID: "tx-create",
			Payload: map[string]any{
				"TypeSlug": "note", "CreatedBy": agent, "AccountID": "",
				"Data": map[string]any{"@id": aggregateID, "name": "May statement"},
			},
		},
		{
			ID: aggregateID + "-pub", EventType: "Resource.Published", AggregateID: aggregateID,
			SequenceNo: 2, TransactionID: "tx-create",
			Payload: map[string]any{"TypeSlug": "note", "AccountID": ""},
		},
	}
}

func accountAssignedEnvelope(aggregateID, account string, seq int) domain.EventEnvelope[any] {
	return domain.EventEnvelope[any]{
		ID: aggregateID + "-assigned", EventType: "Resource.AccountAssigned", AggregateID: aggregateID,
		SequenceNo: seq, TransactionID: "tx-assign",
		Payload: map[string]any{"AccountID": account},
	}
}

func TestAccountFromPayload_AccountAssigned(t *testing.T) {
	t.Parallel()
	if got := accountFromPayload(entities.ResourceAccountAssigned{AccountID: "acct-a"}); got != "acct-a" {
		t.Errorf("typed payload: got %q, want acct-a", got)
	}
	b, err := json.Marshal(entities.ResourceAccountAssigned{AccountID: "acct-a"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if got := accountFromPayload(m); got != "acct-a" {
		t.Errorf("round-tripped payload: got %q, want acct-a (JSON key drift?): %s", got, b)
	}
}

func TestResourceOwnerFromHistory_ReadsTheAssignedOwner(t *testing.T) {
	t.Parallel()
	history := append(ownerlessHistory("urn:note:1", "agent-a"), accountAssignedEnvelope("urn:note:1", "acct-a", 3))
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{"urn:note:1": history}}

	account, createdBy, err := resourceOwnerFromHistory(context.Background(), es, "urn:note:1")
	if err != nil {
		t.Fatalf("owner from history: %v", err)
	}
	if account != "acct-a" || createdBy != "agent-a" {
		t.Fatalf("owner = (%q, %q), want (acct-a, agent-a)", account, createdBy)
	}
}

func TestResourceOwnerFromHistory_AnAssignmentNeverReplacesAnOwner(t *testing.T) {
	t.Parallel()
	history := []domain.EventEnvelope[any]{
		createdEnvelope("acct-a", "agent-a"),
		accountAssignedEnvelope("urn:note:1", "acct-b", 3),
	}
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{"urn:note:1": history}}

	account, _, err := resourceOwnerFromHistory(context.Background(), es, "urn:note:1")
	if err != nil {
		t.Fatalf("owner from history: %v", err)
	}
	if account != "acct-a" {
		t.Fatalf("account = %q, want the first owner acct-a", account)
	}
}

// Before the assignment the per-account projector skipped the resource as an
// accountless write, so it is in no account's graph. The assignment puts it
// into the owner's graph, from the resource's whole history.
func TestProjector_AccountAssignedProjectsTheResourceIntoTheOwnersGraph(t *testing.T) {
	t.Parallel()
	assigned := accountAssignedEnvelope("urn:note:1", "acct-a", 3)
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(ownerlessHistory("urn:note:1", "agent-a"), assigned),
	}}
	store := &fakeKGStore{active: true}
	stores := &fakeStores{perAccount: true, active: true, store: store}
	proj := newOxigraphProjector(es, stores, &kgTypeRepo{rt: makeRT("note", `{"@vocab":"https://schema.org/"}`)}, noopLogger{})

	if err := proj.handle(context.Background(), assigned); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if stores.lastAccount != "acct-a" {
		t.Fatalf("routed to %q, want acct-a", stores.lastAccount)
	}
	if len(store.loadedFormats) != 1 {
		t.Fatalf("loaded %d document(s) into the owner's graph, want 1", len(store.loadedFormats))
	}
}

// A graph rebuild replays the resource's old events, which carry no account.
// After the assignment they route to the owner instead of being skipped.
func TestProjector_OldEventsRouteToTheAssignedOwner(t *testing.T) {
	t.Parallel()
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(ownerlessHistory("urn:note:1", "agent-a"),
			accountAssignedEnvelope("urn:note:1", "acct-a", 3)),
	}}
	proj := newOxigraphProjector(es, &fakeStores{perAccount: true, active: true}, &kgTypeRepo{}, noopLogger{})

	got, err := proj.accountForEvent(context.Background(),
		envWithPayload("Triple.Created", "urn:note:1", map[string]any{"AccountID": ""}))
	if err != nil || got != "acct-a" {
		t.Fatalf("got (%q, %v), want (acct-a, nil)", got, err)
	}
}

func TestProjector_AccountAssignedForADeletedResourceProjectsNothing(t *testing.T) {
	t.Parallel()
	deleted := domain.EventEnvelope[any]{
		ID: "del", EventType: "Resource.Deleted", AggregateID: "urn:note:1", SequenceNo: 3,
		TransactionID: "tx-del", Payload: map[string]any{"AccountID": ""},
	}
	assigned := accountAssignedEnvelope("urn:note:1", "acct-a", 4)
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(ownerlessHistory("urn:note:1", "agent-a"), deleted, assigned),
	}}
	store := &fakeKGStore{active: true}
	proj := newOxigraphProjector(es, &fakeStores{perAccount: true, active: true, store: store},
		&kgTypeRepo{rt: makeRT("note", `{"@vocab":"https://schema.org/"}`)}, noopLogger{})
	if err := proj.handle(context.Background(), assigned); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(store.loadedFormats) != 0 {
		t.Fatalf("a deleted resource was loaded into the graph")
	}
}

// The lexical index row keeps its text and gains the owner, so an
// account-scoped keyword search finds the resource.
func TestLexicalHandler_AccountAssignedReindexesWithTheOwner(t *testing.T) {
	t.Parallel()
	assigned := accountAssignedEnvelope("urn:note:1", "acct-a", 3)
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(ownerlessHistory("urn:note:1", "agent-a"), assigned),
	}}
	index := newFakeLexicalIndex(true)
	index.indexed["urn:note:1"] = "May statement"
	index.types["urn:note:1"] = "note"
	index.owners["urn:note:1"] = repositories.LexicalOwner{CreatedBy: "agent-a"}

	if err := lexicalHandler(es, index, noopLogger{})(context.Background(), assigned); err != nil {
		t.Fatalf("handler: %v", err)
	}
	want := repositories.LexicalOwner{CreatedBy: "agent-a", AccountID: "acct-a"}
	if got := index.owners["urn:note:1"]; got != want {
		t.Fatalf("owner = %+v, want %+v", got, want)
	}
	if index.indexed["urn:note:1"] != "May statement" {
		t.Fatalf("content = %q, want the resource's text kept", index.indexed["urn:note:1"])
	}
	if index.types["urn:note:1"] != "note" {
		t.Fatalf("type = %q, want note", index.types["urn:note:1"])
	}
}

func TestTypedForReplay_AccountAssigned(t *testing.T) {
	t.Parallel()
	env, handled, err := typedForReplay(accountAssignedEnvelope("urn:note:1", "acct-a", 3))
	if err != nil || !handled {
		t.Fatalf("typedForReplay = (handled %v, err %v), want a handled event", handled, err)
	}
	payload, ok := env.Payload.(entities.ResourceAccountAssigned)
	if !ok {
		t.Fatalf("payload type = %T, want entities.ResourceAccountAssigned", env.Payload)
	}
	if payload.AccountID != "acct-a" {
		t.Fatalf("account = %q, want acct-a", payload.AccountID)
	}
}
