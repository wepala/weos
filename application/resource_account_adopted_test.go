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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// These tests pin how every consumer of Resource.AccountAdopted (wm-c7xehr)
// follows it: compare-and-set on FromAccountID, so a replay over a moved
// resource, or the event met by a resource of a third account, changes
// nothing.

const (
	orphanAcct = "acct-orphan"
	targetAcct = "acct-target"
	thirdAcct  = "acct-third"
)

// historyIn is a resource created in account by agent.
func historyIn(aggregateID, agent, account string) []domain.EventEnvelope[any] {
	return []domain.EventEnvelope[any]{
		{
			ID: aggregateID + "-created", EventType: "Resource.Created", AggregateID: aggregateID,
			SequenceNo: 1, TransactionID: "tx-create",
			Payload: map[string]any{
				"TypeSlug": "note", "CreatedBy": agent, "AccountID": account,
				"Data": map[string]any{"@id": aggregateID, "name": "June statement"},
			},
		},
		{
			ID: aggregateID + "-pub", EventType: "Resource.Published", AggregateID: aggregateID,
			SequenceNo: 2, TransactionID: "tx-create",
			Payload: map[string]any{"TypeSlug": "note", "AccountID": account},
		},
	}
}

func adoptedEnv(aggregateID, from, to string, seq int) domain.EventEnvelope[any] {
	return domain.EventEnvelope[any]{
		ID: fmt.Sprintf("%s-adopted-%d", aggregateID, seq), EventType: "Resource.AccountAdopted",
		AggregateID: aggregateID, SequenceNo: seq, TransactionID: fmt.Sprintf("tx-adopt-%d", seq),
		Payload: map[string]any{"FromAccountID": from, "AccountID": to},
	}
}

func TestAccountFromPayload_AccountAdoptedRoutesToTheNewOwner(t *testing.T) {
	t.Parallel()
	typed := entities.ResourceAccountAdopted{FromAccountID: orphanAcct, AccountID: targetAcct}
	if got := accountFromPayload(typed); got != targetAcct {
		t.Errorf("typed payload: got %q, want %s", got, targetAcct)
	}
	b, err := json.Marshal(typed)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if got := accountFromPayload(m); got != targetAcct {
		t.Errorf("round-tripped payload: got %q, want %s: %s", got, targetAcct, b)
	}
	if from, to := adoptedAccounts(m); from != orphanAcct || to != targetAcct {
		t.Errorf("adoptedAccounts = (%q, %q), want (%s, %s)", from, to, orphanAcct, targetAcct)
	}
}

func TestResourceOwnerFromHistory_FollowsAnAdoption(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		history []domain.EventEnvelope[any]
		want    string
	}{
		{"moved out of the source", append(historyIn("urn:note:1", "agent-a", orphanAcct),
			adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3)), targetAcct},
		{"a move that names another source changes nothing", append(historyIn("urn:note:1", "agent-a", thirdAcct),
			adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3)), thirdAcct},
		{"a second copy of the move changes nothing", append(historyIn("urn:note:1", "agent-a", orphanAcct),
			adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3),
			adoptedEnv("urn:note:1", orphanAcct, "acct-elsewhere", 4)), targetAcct},
		{"assigned, then moved", append(ownerlessHistory("urn:note:1", "agent-a"),
			accountAssignedEnvelope("urn:note:1", orphanAcct, 3),
			adoptedEnv("urn:note:1", orphanAcct, targetAcct, 4)), targetAcct},
	} {
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{"urn:note:1": c.history}}
		account, createdBy, err := resourceOwnerFromHistory(context.Background(), es, "urn:note:1")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if account != c.want || createdBy != "agent-a" {
			t.Errorf("%s: owner = (%q, %q), want (%q, agent-a)", c.name, account, createdBy, c.want)
		}
	}
}

func TestTypedForReplay_AccountAdopted(t *testing.T) {
	t.Parallel()
	env, handled, err := typedForReplay(adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3))
	if err != nil || !handled {
		t.Fatalf("typedForReplay = (handled %v, err %v), want a handled event", handled, err)
	}
	payload, ok := env.Payload.(entities.ResourceAccountAdopted)
	if !ok {
		t.Fatalf("payload type = %T, want entities.ResourceAccountAdopted", env.Payload)
	}
	if payload.FromAccountID != orphanAcct || payload.AccountID != targetAcct {
		t.Fatalf("payload = %+v", payload)
	}
}

// accountStores is a per-account graph with one fake store per account.
type accountStores struct {
	mu     sync.Mutex
	stores map[string]*fakeKGStore
}

func newAccountStores() *accountStores {
	return &accountStores{stores: map[string]*fakeKGStore{}}
}

func (a *accountStores) Active() bool     { return true }
func (a *accountStores) PerAccount() bool { return true }
func (a *accountStores) ForAccount(_ context.Context, id string) (repositories.KnowledgeGraphStore, error) {
	return a.store(id), nil
}
func (a *accountStores) store(id string) *fakeKGStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.stores[id]; ok {
		return s
	}
	s := &fakeKGStore{active: true}
	a.stores[id] = s
	return s
}
func (a *accountStores) Truncate(context.Context) error                      { return nil }
func (a *accountStores) DropAccount(context.Context, string, []string) error { return nil }
func (a *accountStores) Close() error                                        { return nil }

func noteType() *kgTypeRepo {
	return &kgTypeRepo{rt: makeRT("note", `{"@vocab":"https://schema.org/"}`)}
}

// The move takes the resource out of the source account's graph and puts
// its whole state into the target's.
func TestProjector_AccountAdoptedMovesTheResourceBetweenGraphs(t *testing.T) {
	t.Parallel()
	adopted := adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3)
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(historyIn("urn:note:1", "agent-a", orphanAcct), adopted),
	}}
	stores := newAccountStores()
	proj := newOxigraphProjector(es, stores, noteType(), noopLogger{})

	if err := proj.handle(context.Background(), adopted); err != nil {
		t.Fatalf("handle: %v", err)
	}
	from, to := stores.store(orphanAcct), stores.store(targetAcct)
	if len(from.subjectsRemoved) != 1 || from.subjectsRemoved[0] != "urn:note:1" {
		t.Errorf("source graph removed %v, want the resource", from.subjectsRemoved)
	}
	if len(from.loadedFormats) != 0 {
		t.Errorf("source graph loaded %d document(s), want none", len(from.loadedFormats))
	}
	// The target graph gets the type's ontology and the resource.
	if len(to.loadedFormats) < 1 {
		t.Fatalf("target graph loaded nothing, want the resource")
	}
}

// A replay of the move over a resource the stream already shows in another
// account (a third account, or a second copy of the move) touches no graph.
func TestProjector_AccountAdoptedIsCompareAndSet(t *testing.T) {
	t.Parallel()
	for name, history := range map[string][]domain.EventEnvelope[any]{
		"a resource of a third account": append(historyIn("urn:note:1", "agent-a", thirdAcct),
			adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3)),
		"a second copy of the move": append(historyIn("urn:note:1", "agent-a", orphanAcct),
			adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3),
			adoptedEnv("urn:note:1", orphanAcct, targetAcct, 4)),
	} {
		last := history[len(history)-1]
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{"urn:note:1": history}}
		stores := newAccountStores()
		proj := newOxigraphProjector(es, stores, noteType(), noopLogger{})
		if err := proj.handle(context.Background(), last); err != nil {
			t.Fatalf("%s: handle: %v", name, err)
		}
		for account, s := range stores.stores {
			if len(s.subjectsRemoved) != 0 || len(s.loadedFormats) != 0 {
				t.Errorf("%s: graph of %s changed: removed %v, loaded %d", name, account,
					s.subjectsRemoved, len(s.loadedFormats))
			}
		}
	}
}

// A graph rebuild replays the move in order: the resource's earlier events
// land in the source graph, the move takes it out, and later events carry
// the new owner.
func TestProjector_EventsAfterTheMoveRouteToTheTarget(t *testing.T) {
	t.Parallel()
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(historyIn("urn:note:1", "agent-a", orphanAcct),
			adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3)),
	}}
	proj := newOxigraphProjector(es, newAccountStores(), noteType(), noopLogger{})
	got, err := proj.accountForEvent(context.Background(),
		envWithPayload("Triple.Created", "urn:note:1", map[string]any{"AccountID": ""}))
	if err != nil || got != targetAcct {
		t.Fatalf("got (%q, %v), want (%s, nil)", got, err, targetAcct)
	}
}

func TestProjector_AccountAdoptedForADeletedResourceOnlyLeavesTheSource(t *testing.T) {
	t.Parallel()
	deleted := domain.EventEnvelope[any]{
		ID: "del", EventType: "Resource.Deleted", AggregateID: "urn:note:1", SequenceNo: 3,
		TransactionID: "tx-del", Payload: map[string]any{"AccountID": orphanAcct},
	}
	adopted := adoptedEnv("urn:note:1", orphanAcct, targetAcct, 4)
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(historyIn("urn:note:1", "agent-a", orphanAcct), deleted, adopted),
	}}
	stores := newAccountStores()
	proj := newOxigraphProjector(es, stores, noteType(), noopLogger{})
	if err := proj.handle(context.Background(), adopted); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if n := len(stores.store(targetAcct).loadedFormats); n != 0 {
		t.Fatalf("a deleted resource was loaded into the target graph (%d document(s))", n)
	}
}

func TestLexicalHandler_AccountAdoptedReindexesUnderTheTarget(t *testing.T) {
	t.Parallel()
	adopted := adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3)
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(historyIn("urn:note:1", "agent-a", orphanAcct), adopted),
	}}
	index := newFakeLexicalIndex(true)
	index.indexed["urn:note:1"] = "June statement"
	index.types["urn:note:1"] = "note"
	index.owners["urn:note:1"] = repositories.LexicalOwner{CreatedBy: "agent-a", AccountID: orphanAcct}

	if err := lexicalHandler(es, index, noopLogger{})(context.Background(), adopted); err != nil {
		t.Fatalf("handler: %v", err)
	}
	want := repositories.LexicalOwner{CreatedBy: "agent-a", AccountID: targetAcct}
	if got := index.owners["urn:note:1"]; got != want {
		t.Fatalf("owner = %+v, want %+v", got, want)
	}
	if index.indexed["urn:note:1"] != "June statement" || index.types["urn:note:1"] != "note" {
		t.Fatalf("row = (%q, %q), want the text and type kept", index.indexed["urn:note:1"], index.types["urn:note:1"])
	}
}

func TestLexicalHandler_AccountAdoptedLeavesARowOfAnotherAccount(t *testing.T) {
	t.Parallel()
	adopted := adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3)
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(historyIn("urn:note:1", "agent-a", thirdAcct), adopted),
	}}
	index := newFakeLexicalIndex(true)
	index.indexed["urn:note:1"] = "June statement"
	index.owners["urn:note:1"] = repositories.LexicalOwner{CreatedBy: "agent-a", AccountID: thirdAcct}

	if err := lexicalHandler(es, index, noopLogger{})(context.Background(), adopted); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if got := index.owners["urn:note:1"].AccountID; got != thirdAcct {
		t.Fatalf("owner = %q, want the third account kept", got)
	}
}

// adoptRepo is a ResourceRepository with a canonical record and one type
// projection row per resource. WriteAccountAdoption writes the record's
// owner and version and moves the type row's owner compare-and-set, as the
// gorm repository does. Update fails: the adoption must never rebuild a
// whole row, which recomputes its reference labels in the target account
// (wm-xuskm9).
type adoptRepo struct {
	repositories.ResourceRepository
	record     map[string]string // id → record owner
	seq        map[string]int
	typeRow    map[string]string // id → type row owner
	moveCalls  int
	updateSeqs []int
}

func newAdoptRepo(id, recordOwner, rowOwner string) *adoptRepo {
	return &adoptRepo{
		record: map[string]string{id: recordOwner}, seq: map[string]int{id: 2},
		typeRow: map[string]string{id: rowOwner},
	}
}

func (r *adoptRepo) FindByID(_ context.Context, id string) (*entities.Resource, error) {
	owner, ok := r.record[id]
	if !ok {
		return nil, fmt.Errorf("resource %q: %w", id, repositories.ErrNotFound)
	}
	e := &entities.Resource{}
	if err := e.Restore(id, "note", "active", json.RawMessage(`{"name":"x"}`), "agent-a", owner,
		time.Now(), r.seq[id]); err != nil {
		return nil, err
	}
	return e, nil
}

func (r *adoptRepo) Update(_ context.Context, e *entities.Resource) error {
	return fmt.Errorf("Update(%s) called: the adoption must write only the owner and the version", e.GetID())
}

func (r *adoptRepo) WriteAccountAdoption(
	_ context.Context, e *entities.Resource, from string, move bool,
) error {
	id := e.GetID()
	r.record[id] = e.AccountID()
	r.seq[id] = e.GetSequenceNo()
	r.updateSeqs = append(r.updateSeqs, e.GetSequenceNo())
	if move {
		r.moveCalls++
	}
	if row := r.typeRow[id]; row == "" || (move && row == from) {
		r.typeRow[id] = e.AccountID()
	}
	return nil
}

func adoptedTyped(id, from, to string, seq int) domain.EventEnvelope[entities.ResourceAccountAdopted] {
	return domain.EventEnvelope[entities.ResourceAccountAdopted]{
		AggregateID: id, EventType: "Resource.AccountAdopted", SequenceNo: seq,
		Payload: entities.ResourceAccountAdopted{FromAccountID: from, AccountID: to},
	}
}

func TestHandleResourceAccountAdopted_MovesTheRecordAndEveryRow(t *testing.T) {
	t.Parallel()
	repo := newAdoptRepo("urn:note:1", orphanAcct, orphanAcct)
	if err := handleResourceAccountAdopted(context.Background(),
		adoptedTyped("urn:note:1", orphanAcct, targetAcct, 3), repo, noopLogger{}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if repo.record["urn:note:1"] != targetAcct || repo.typeRow["urn:note:1"] != targetAcct {
		t.Fatalf("owners = record %q, row %q; want %s in both", repo.record["urn:note:1"],
			repo.typeRow["urn:note:1"], targetAcct)
	}
	if repo.seq["urn:note:1"] != 3 {
		t.Fatalf("record sequence = %d, want the event's 3", repo.seq["urn:note:1"])
	}
}

// A record a rebuild just wrote back into the source account (the replayed
// Resource.Created) while its row kept the target is moved again, and the
// row stays where it is.
func TestHandleResourceAccountAdopted_AReplayOverMovedRowsConverges(t *testing.T) {
	t.Parallel()
	repo := newAdoptRepo("urn:note:1", orphanAcct, targetAcct)
	if err := handleResourceAccountAdopted(context.Background(),
		adoptedTyped("urn:note:1", orphanAcct, targetAcct, 3), repo, noopLogger{}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if repo.record["urn:note:1"] != targetAcct || repo.typeRow["urn:note:1"] != targetAcct {
		t.Fatalf("owners = record %q, row %q; want %s in both", repo.record["urn:note:1"],
			repo.typeRow["urn:note:1"], targetAcct)
	}
}

// A resource of a third account keeps its owner on the record and on the
// row; only the version moves on, so the next write appends after it.
func TestHandleResourceAccountAdopted_LeavesAResourceOfAThirdAccount(t *testing.T) {
	t.Parallel()
	repo := newAdoptRepo("urn:note:1", thirdAcct, thirdAcct)
	if err := handleResourceAccountAdopted(context.Background(),
		adoptedTyped("urn:note:1", orphanAcct, targetAcct, 3), repo, noopLogger{}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if repo.record["urn:note:1"] != thirdAcct || repo.typeRow["urn:note:1"] != thirdAcct {
		t.Fatalf("owners = record %q, row %q; want %s kept", repo.record["urn:note:1"],
			repo.typeRow["urn:note:1"], thirdAcct)
	}
	if repo.moveCalls != 0 {
		t.Fatalf("moved rows %d time(s) for a resource of a third account", repo.moveCalls)
	}
	if repo.seq["urn:note:1"] != 3 {
		t.Fatalf("record sequence = %d, want the event's 3", repo.seq["urn:note:1"])
	}
}

func TestHandleResourceAccountAdopted_ARemovedRecordIsSkipped(t *testing.T) {
	t.Parallel()
	repo := &adoptRepo{record: map[string]string{}, seq: map[string]int{}, typeRow: map[string]string{}}
	if err := handleResourceAccountAdopted(context.Background(),
		adoptedTyped("urn:note:1", orphanAcct, targetAcct, 3), repo, noopLogger{}); err != nil {
		t.Fatalf("handle: %v", err)
	}
}

// wm-ebgqob (5): RemoveSubject takes the resource's relation triples out of
// the source graph with the rest of it, so the move carries the relations
// its history still holds (each Triple.Created not undone by a later
// Triple.Deleted) into the target graph.
func TestProjector_AccountAdoptedCarriesTheRelationsToTheTarget(t *testing.T) {
	t.Parallel()
	triple := func(seq int, eventType, object string) domain.EventEnvelope[any] {
		return domain.EventEnvelope[any]{
			ID: fmt.Sprintf("urn:note:1-triple-%d", seq), EventType: eventType, AggregateID: "urn:note:1",
			SequenceNo: seq, TransactionID: fmt.Sprintf("tx-%d", seq),
			Payload: map[string]any{"subject": "urn:note:1", "predicate": "https://schema.org/about",
				"object": object, "AccountID": orphanAcct},
		}
	}
	history := append(historyIn("urn:note:1", "agent-a", orphanAcct),
		triple(3, "Triple.Created", "urn:project:kept"),
		triple(4, "Triple.Created", "urn:project:dropped"),
		triple(5, "Triple.Deleted", "urn:project:dropped"))
	adopted := adoptedEnv("urn:note:1", orphanAcct, targetAcct, 6)
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{"urn:note:1": append(history, adopted)}}
	stores := newAccountStores()
	proj := newOxigraphProjector(es, stores, noteType(), noopLogger{})
	if err := proj.handle(context.Background(), adopted); err != nil {
		t.Fatalf("handle: %v", err)
	}
	var added []repositories.Triple
	for _, call := range stores.store(targetAcct).addCalls {
		for _, tr := range call {
			if tr.Subject == "urn:note:1" { // the ontology's own triples are not relations
				added = append(added, tr)
			}
		}
	}
	want := repositories.Triple{Subject: "urn:note:1", Predicate: "https://schema.org/about", Object: "urn:project:kept"}
	if len(added) != 1 || added[0] != want {
		t.Fatalf("target graph got relations %+v, want only %+v", added, want)
	}
}

// Copilot 4580547731: before the move removes the resource from the source
// graph it removes the blank nodes nested below it, and only those — a
// triple of another resource that points at the moved one is that
// resource's, so nothing of the form ?s ?p <urn> is deleted.
func TestProjector_AccountAdoptedRemovesTheNestedNodesFromTheSource(t *testing.T) {
	t.Parallel()
	adopted := adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3)
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(historyIn("urn:note:1", "agent-a", orphanAcct), adopted),
	}}
	stores := newAccountStores()
	proj := newOxigraphProjector(es, stores, noteType(), noopLogger{})
	if err := proj.handle(context.Background(), adopted); err != nil {
		t.Fatalf("handle: %v", err)
	}
	source := stores.store(orphanAcct)
	nested := 0
	for _, u := range source.updates {
		switch {
		case strings.HasPrefix(u, "DELETE { ?b") && strings.Contains(u, "<urn:note:1> ?q1 ?b1 . FILTER(isBlank(?b1))"):
			nested++
		default:
			t.Errorf("the move ran an update on the source graph that is not a nested-node delete: %s", u)
		}
	}
	if nested == 0 {
		t.Errorf("the move removed no nested nodes from the source graph; updates %v", source.updates)
	}
	if len(source.subjectsRemoved) != 1 || source.subjectsRemoved[0] != "urn:note:1" {
		t.Errorf("source graph removed %v, want the resource", source.subjectsRemoved)
	}
}

// laterEnv is an event of the resource after the move: a delete, or an
// update that replaces its data.
func laterEnv(aggregateID, eventType string, seq int, account string, data map[string]any) domain.EventEnvelope[any] {
	payload := map[string]any{"AccountID": account}
	if data != nil {
		payload["Data"] = data
	}
	return domain.EventEnvelope[any]{
		ID: fmt.Sprintf("%s-%s-%d", aggregateID, eventType, seq), EventType: eventType,
		AggregateID: aggregateID, SequenceNo: seq, TransactionID: fmt.Sprintf("tx-later-%d", seq),
		Payload: payload,
	}
}

// Copilot review 5913504768 (lexical_group.go): the worker goes on past a
// parked event, so `worker parked replay` can run a move after later events
// of the resource already ran. The replay must leave the index as those
// events left it, and never bring back what they took out.
func TestLexicalHandler_AReplayedParkedAdoptionHonorsLaterEvents(t *testing.T) {
	t.Parallel()
	const id = "urn:note:1"
	adopted := adoptedEnv(id, orphanAcct, targetAcct, 3)

	t.Run("a later delete leaves no search row", func(t *testing.T) {
		t.Parallel()
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
			id: append(historyIn(id, "agent-a", orphanAcct), adopted,
				laterEnv(id, "Resource.Deleted", 4, targetAcct, nil)),
		}}
		index := newFakeLexicalIndex(true) // the delete removed the row
		if err := lexicalHandler(es, index, noopLogger{})(context.Background(), adopted); err != nil {
			t.Fatalf("handler: %v", err)
		}
		if text, ok := index.indexed[id]; ok {
			t.Fatalf("the replay re-created the search row of a deleted resource: %q", text)
		}
	})

	t.Run("a later update that emptied the text leaves no search row", func(t *testing.T) {
		t.Parallel()
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
			id: append(historyIn(id, "agent-a", orphanAcct), adopted,
				laterEnv(id, "Resource.Updated", 4, targetAcct, map[string]any{"@id": id})),
		}}
		index := newFakeLexicalIndex(true) // the empty update removed the row
		if err := lexicalHandler(es, index, noopLogger{})(context.Background(), adopted); err != nil {
			t.Fatalf("handler: %v", err)
		}
		if text, ok := index.indexed[id]; ok {
			t.Fatalf("the replay re-created a search row the later update removed: %q", text)
		}
	})

	t.Run("a later update keeps its newer text", func(t *testing.T) {
		t.Parallel()
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
			id: append(historyIn(id, "agent-a", orphanAcct), adopted,
				laterEnv(id, "Resource.Updated", 4, targetAcct,
					map[string]any{"@id": id, "name": "July statement"})),
		}}
		index := newFakeLexicalIndex(true)
		// The update ran while the move was parked, so the row still has the
		// source account and already has the newer text.
		index.indexed[id] = "July statement"
		index.types[id] = "note"
		index.owners[id] = repositories.LexicalOwner{CreatedBy: "agent-a", AccountID: orphanAcct}
		if err := lexicalHandler(es, index, noopLogger{})(context.Background(), adopted); err != nil {
			t.Fatalf("handler: %v", err)
		}
		if got := index.indexed[id]; got != "July statement" {
			t.Fatalf("search text = %q, want the later update's %q", got, "July statement")
		}
		if got := index.owners[id].AccountID; got != targetAcct {
			t.Fatalf("owner = %q, want the target %q", got, targetAcct)
		}
	})

	t.Run("two parked moves replayed in order end in the last account", func(t *testing.T) {
		t.Parallel()
		second := adoptedEnv(id, targetAcct, thirdAcct, 4)
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
			id: append(historyIn(id, "agent-a", orphanAcct), adopted, second),
		}}
		index := newFakeLexicalIndex(true)
		index.indexed[id] = "June statement"
		index.types[id] = "note"
		index.owners[id] = repositories.LexicalOwner{CreatedBy: "agent-a", AccountID: orphanAcct}
		handle := lexicalHandler(es, index, noopLogger{})
		for _, e := range []domain.EventEnvelope[any]{adopted, second} {
			if err := handle(context.Background(), e); err != nil {
				t.Fatalf("handler: %v", err)
			}
		}
		if got := index.owners[id].AccountID; got != thirdAcct {
			t.Fatalf("owner = %q, want the last move's %q", got, thirdAcct)
		}
	})
}

// Copilot review 5913504768 (peripheral_groups.go): `worker parked replay`
// can run a move after the resource's later events already reached the
// graphs. The replay still takes the resource out of the source graph, but
// it never restores in the target graph what a later delete or a later move
// took out of it, and it never loads data older than a later update.
func TestProjector_AReplayedParkedAdoptionHonorsLaterEvents(t *testing.T) {
	t.Parallel()
	const id = "urn:note:1"
	adopted := adoptedEnv(id, orphanAcct, targetAcct, 3)
	replay := func(t *testing.T, later ...domain.EventEnvelope[any]) *accountStores {
		t.Helper()
		history := append(append(historyIn(id, "agent-a", orphanAcct), adopted), later...)
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{id: history}}
		stores := newAccountStores()
		if err := newOxigraphProjector(es, stores, noteType(), noopLogger{}).handle(context.Background(), adopted); err != nil {
			t.Fatalf("handle: %v", err)
		}
		if removed := stores.store(orphanAcct).subjectsRemoved; len(removed) != 1 || removed[0] != id {
			t.Errorf("source graph removed %v, want the resource", removed)
		}
		return stores
	}

	t.Run("a later delete leaves the resource out of the target graph", func(t *testing.T) {
		t.Parallel()
		stores := replay(t, laterEnv(id, "Resource.Deleted", 4, targetAcct, nil))
		if docs := stores.store(targetAcct).loadedDocs; len(docs) != 0 {
			t.Fatalf("the replay restored a deleted resource in the target graph: %v", docs)
		}
	})

	t.Run("a later move leaves the resource out of the target graph", func(t *testing.T) {
		t.Parallel()
		stores := replay(t, adoptedEnv(id, targetAcct, thirdAcct, 4))
		if docs := stores.store(targetAcct).loadedDocs; len(docs) != 0 {
			t.Fatalf("the replay restored in the target graph a resource a later move took out: %v", docs)
		}
	})

	t.Run("a later update is the data the target graph gets", func(t *testing.T) {
		t.Parallel()
		stores := replay(t, laterEnv(id, "Resource.Updated", 4, targetAcct,
			map[string]any{"@id": id, "name": "July statement"}))
		docs := stores.store(targetAcct).loadedDocs
		if len(docs) == 0 {
			t.Fatalf("the target graph got nothing, want the resource")
		}
		last := docs[len(docs)-1]
		if !strings.Contains(last, "July statement") || strings.Contains(last, "June statement") {
			t.Fatalf("the target graph got %s, want the later update's data", last)
		}
	})
}

// The graph handler's compare-and-set reads the history, not the graph, so
// two moves processed in order end with the resource in the last account's
// graph only, and a move replayed after a later move brought the resource
// back to the source leaves the source graph alone.
func TestProjector_TwoAdoptionsInOrderEndInTheLastAccount(t *testing.T) {
	t.Parallel()
	const id = "urn:note:1"
	first := adoptedEnv(id, orphanAcct, targetAcct, 3)

	t.Run("on to a third account", func(t *testing.T) {
		t.Parallel()
		second := adoptedEnv(id, targetAcct, thirdAcct, 4)
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
			id: append(historyIn(id, "agent-a", orphanAcct), first, second),
		}}
		stores := newAccountStores()
		proj := newOxigraphProjector(es, stores, noteType(), noopLogger{})
		for _, e := range []domain.EventEnvelope[any]{first, second} {
			if err := proj.handle(context.Background(), e); err != nil {
				t.Fatalf("handle: %v", err)
			}
		}
		if docs := stores.store(targetAcct).loadedDocs; len(docs) != 0 {
			t.Errorf("the target graph got %v, want nothing: the resource moved on", docs)
		}
		if docs := stores.store(thirdAcct).loadedDocs; len(docs) == 0 {
			t.Errorf("the third account's graph got nothing, want the resource")
		}
	})

	t.Run("back to the source", func(t *testing.T) {
		t.Parallel()
		back := adoptedEnv(id, targetAcct, orphanAcct, 4)
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
			id: append(historyIn(id, "agent-a", orphanAcct), first, back),
		}}
		stores := newAccountStores()
		if err := newOxigraphProjector(es, stores, noteType(), noopLogger{}).handle(context.Background(), first); err != nil {
			t.Fatalf("handle: %v", err)
		}
		for account, s := range stores.stores {
			if len(s.subjectsRemoved) != 0 || len(s.loadedDocs) != 0 || len(s.updates) != 0 {
				t.Errorf("graph of %s changed: removed %v, loaded %v, updates %v", account,
					s.subjectsRemoved, s.loadedDocs, s.updates)
			}
		}
	})
}

// Copilot review 5913504768 (peripheral_groups.go:299): before the move
// loads the resource into the target graph it removes the nested nodes a
// first run of the same move left there, and only the resource's own.
func TestProjector_AccountAdoptedClearsTheTargetsNestedNodesBeforeItLoads(t *testing.T) {
	t.Parallel()
	adopted := adoptedEnv("urn:note:1", orphanAcct, targetAcct, 3)
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:note:1": append(historyIn("urn:note:1", "agent-a", orphanAcct), adopted),
	}}
	stores := newAccountStores()
	if err := newOxigraphProjector(es, stores, noteType(), noopLogger{}).handle(context.Background(), adopted); err != nil {
		t.Fatalf("handle: %v", err)
	}
	nested := 0
	for _, u := range stores.store(targetAcct).updates {
		if strings.HasPrefix(u, "DELETE { ?b") && strings.Contains(u, "<urn:note:1> ?q1 ?b1 . FILTER(isBlank(?b1))") {
			nested++
			continue
		}
		t.Errorf("the move ran an update on the target graph that is not a nested-node delete: %s", u)
	}
	if nested == 0 {
		t.Errorf("the move cleared no nested nodes in the target graph before loading it")
	}
}
