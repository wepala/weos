//go:build oxigraph_embedded

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
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/graph/oxigraph"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// embeddedAccountStores is a per-account graph with one real embedded
// oxigraph store per account.
type embeddedAccountStores struct {
	t      *testing.T
	mu     sync.Mutex
	stores map[string]repositories.KnowledgeGraphStore
}

func (a *embeddedAccountStores) Active() bool     { return true }
func (a *embeddedAccountStores) PerAccount() bool { return true }
func (a *embeddedAccountStores) ForAccount(_ context.Context, id string) (repositories.KnowledgeGraphStore, error) {
	return a.store(id), nil
}
func (a *embeddedAccountStores) store(id string) repositories.KnowledgeGraphStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.stores[id]; ok {
		return s
	}
	s, err := oxigraph.NewEmbeddedStore(filepath.Join(a.t.TempDir(), id), nil)
	if err != nil {
		a.t.Fatalf("NewEmbeddedStore(%s): %v", id, err)
	}
	a.t.Cleanup(func() {
		if c, ok := s.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	})
	a.stores[id] = s
	return s
}
func (a *embeddedAccountStores) Truncate(context.Context) error                      { return nil }
func (a *embeddedAccountStores) DropAccount(context.Context, string, []string) error { return nil }
func (a *embeddedAccountStores) Close() error                                        { return nil }

// graphStatements lists every triple in a store as "s p o", with blank nodes
// written as _:b so two graphs can be compared.
func graphStatements(t *testing.T, store repositories.KnowledgeGraphStore) []string {
	t.Helper()
	res, err := store.Query(context.Background(), "SELECT ?s ?p ?o WHERE { ?s ?p ?o }")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	term := func(k repositories.KGTerm) string {
		if k.Type == repositories.KGTermBlank {
			return "_:b"
		}
		return k.Value
	}
	var out []string
	for _, b := range res.Bindings {
		out = append(out, term(b["s"])+" "+term(b["p"])+" "+term(b["o"]))
	}
	sort.Strings(out)
	return out
}

// Copilot 4580547731: a resource document with nested objects — an
// ingredient list, a nutrition block with a nested serving — is projected as
// blank nodes reachable only from the resource. The move takes them out of
// the source graph with the resource, puts the same nested data in the
// target graph, and leaves another resource the moved one points at.
func TestEmbeddedAccountAdopted_MovesTheNestedNodesWithTheResource(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{
		"@context": map[string]any{"@vocab": "https://schema.org/"},
		"@id":      "urn:recipe:1", "@type": "Recipe", "name": "Pepperpot",
		"author":            map[string]any{"@id": "urn:person:chef"},
		"recipeIngredient":  []any{map[string]any{"name": "Cassareep"}, map[string]any{"name": "Beef"}},
		"nutrition":         map[string]any{"calories": "540", "servingSize": map[string]any{"value": "1 bowl"}},
		"recipeInstruction": map[string]any{"text": "Simmer for three hours"},
	}
	history := []domain.EventEnvelope[any]{
		{
			ID: "c", EventType: "Resource.Created", AggregateID: "urn:recipe:1", SequenceNo: 1, TransactionID: "tx-1",
			Payload: map[string]any{"TypeSlug": "recipe", "CreatedBy": "agent-a", "AccountID": orphanAcct, "Data": data},
		},
		{
			ID: "p", EventType: "Resource.Published", AggregateID: "urn:recipe:1", SequenceNo: 2, TransactionID: "tx-1",
			Payload: map[string]any{"TypeSlug": "recipe", "AccountID": orphanAcct},
		},
	}
	adopted := adoptedEnv("urn:recipe:1", orphanAcct, targetAcct, 3)
	es := &fakeEventStore{
		byAggregate: map[string][]domain.EventEnvelope[any]{"urn:recipe:1": append(history, adopted)},
		tx:          map[string][]domain.EventEnvelope[any]{"tx-1": history},
	}
	stores := &embeddedAccountStores{t: t, stores: map[string]repositories.KnowledgeGraphStore{}}
	types := &kgTypeRepo{rt: makeRT("recipe", `{"@vocab":"https://schema.org/"}`)}
	proj := newOxigraphProjector(es, stores, types, noopLogger{})

	if err := proj.handle(ctx, history[1]); err != nil {
		t.Fatalf("publish: %v", err)
	}
	source := stores.store(orphanAcct)
	// The person the recipe names is another resource of the source account.
	chef := repositories.Triple{Subject: "urn:person:chef", Predicate: "https://schema.org/name", Object: `"Ada"`}
	if err := source.AddTriples(ctx, []repositories.Triple{chef}); err != nil {
		t.Fatal(err)
	}
	before := recipeStatements(graphStatements(t, source))
	blanks := 0
	for _, s := range before {
		if strings.HasPrefix(s, "_:b ") {
			blanks++
		}
	}
	if blanks < 5 {
		t.Fatalf("the published recipe holds %d nested statements, want at least 5:\n%v", blanks, before)
	}

	if err := proj.handle(ctx, adopted); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	all := graphStatements(t, source)
	if left := recipeStatements(all); len(left) != 0 {
		t.Errorf("source graph still holds the recipe after the move:\n%v", left)
	}
	if !slices.Contains(all, "urn:person:chef https://schema.org/name Ada") {
		t.Errorf("the move took the chef the recipe names out of the source graph:\n%v", all)
	}
	if got := recipeStatements(graphStatements(t, stores.store(targetAcct))); !slices.Equal(got, before) {
		t.Errorf("target graph holds the recipe as\n%v\nwant the same statements the source held\n%v", got, before)
	}
}

// recipeStatements keeps the statements of the recipe: its own and those of
// its nested (blank) nodes. The type's ontology and other resources are left
// out.
func recipeStatements(all []string) []string {
	var out []string
	for _, s := range all {
		if strings.HasPrefix(s, "_:b ") || strings.HasPrefix(s, "urn:recipe:1 ") {
			out = append(out, s)
		}
	}
	return out
}
