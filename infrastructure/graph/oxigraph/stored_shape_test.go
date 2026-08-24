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

//go:build oxigraph_embedded

package oxigraph_test

import (
	"context"
	"testing"

	"encoding/json"

	"github.com/wepala/weos/v3/infrastructure/graph/oxigraph"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

type shapeTestLogger struct{}

func (shapeTestLogger) Debug(context.Context, string, ...interface{}) {}
func (shapeTestLogger) Info(context.Context, string, ...interface{})  {}
func (shapeTestLogger) Warn(context.Context, string, ...interface{})  {}
func (shapeTestLogger) Error(context.Context, string, ...interface{}) {}

// TestStoredShapesProduceTheSameTriples is the guarantee that issue #515 did
// not move the ontology.
//
// A resource's edges are stored keyed by property name now and by predicate IRI
// before; both shapes are fed to this store as application/ld+json, and both
// must yield the same statements. If they diverge, every SPARQL query and every
// kg_* tool answers differently depending on when a record happened to be
// written — which nothing else in the test suite would notice, because the API
// and the projection both read correctly either way.
//
// Requires the `oxigraph_embedded` build tag, so it does not run in the default
// gate. Run it before changing anything about the stored document shape:
//
//	go test -tags oxigraph_embedded ./infrastructure/graph/oxigraph/
func TestStoredShapesProduceTheSameTriples(t *testing.T) {
	expanded := json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","recipe":"https://schema.org/isPartOf"},
	  "@graph":[
	    {"@id":"urn:meal:1","@type":"Meal","name":"Dinner"},
	    {"@id":"urn:meal:1","https://schema.org/isPartOf":{"@id":"urn:recipe:A"}}
	  ]}`)
	compact := json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","recipe":"https://schema.org/isPartOf"},
	  "@graph":[
	    {"@id":"urn:meal:1","@type":"Meal","name":"Dinner"},
	    {"@id":"urn:meal:1","recipe":{"@id":"urn:recipe:A"}}
	  ]}`)

	expandedTriples := loadAndDescribe(t, expanded)
	compactTriples := loadAndDescribe(t, compact)

	if len(expandedTriples) == 0 {
		t.Fatal("the expanded document produced no triples; the comparison would be vacuous")
	}
	if len(compactTriples) != len(expandedTriples) {
		t.Fatalf("compact produced %d triples, expanded produced %d — the shapes disagree",
			len(compactTriples), len(expandedTriples))
	}
	for triple := range expandedTriples {
		if !compactTriples[triple] {
			t.Errorf("the compact shape lost the statement %s", triple)
		}
	}
}

// TestContextCarryingAtTypeIsRejected pins why buildStorableContext excludes
// `@type`.
//
// `@type` at the top level of a @context is a keyword redefinition, and this
// store refuses the whole document over it — so a resource written that way
// never reaches the knowledge graph at all, while its API read and projection
// look perfectly healthy. That asymmetry is what makes it dangerous: nothing
// else fails, the data is simply absent from every SPARQL answer.
func TestContextCarryingAtTypeIsRejected(t *testing.T) {
	store, err := oxigraph.NewEmbeddedStore(t.TempDir(), shapeTestLogger{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	doc := json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","@type":"Meal"},
	  "@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"Dinner"}]}`)

	if err := store.LoadOntology(
		context.Background(), "application/ld+json", jsonld.InlineVocabContext(doc)); err == nil {
		t.Error("the store accepted @type inside a @context; if that ever becomes valid, " +
			"buildStorableContext's exclusion can be revisited — until then it is load-bearing")
	}
}

// loadAndDescribe loads one document into a fresh store and returns its
// statements as a set, so two shapes can be compared regardless of order.
func loadAndDescribe(t *testing.T, doc json.RawMessage) map[string]bool {
	t.Helper()

	store, err := oxigraph.NewEmbeddedStore(t.TempDir(), shapeTestLogger{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.LoadOntology(
		context.Background(), "application/ld+json", jsonld.InlineVocabContext(doc)); err != nil {
		t.Fatalf("load: %v", err)
	}
	res, err := store.Query(context.Background(), "SELECT ?s ?p ?o WHERE { ?s ?p ?o }")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	out := make(map[string]bool, len(res.Bindings))
	for _, b := range res.Bindings {
		out[b["s"].Value+" "+b["p"].Value+" "+b["o"].Value] = true
	}
	return out
}
