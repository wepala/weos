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

	"io"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/graph/oxigraph"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

type shapeTestLogger struct{}

func (shapeTestLogger) Debug(context.Context, string, ...interface{}) {}
func (shapeTestLogger) Info(context.Context, string, ...interface{})  {}
func (shapeTestLogger) Warn(context.Context, string, ...interface{})  {}
func (shapeTestLogger) Error(context.Context, string, ...interface{}) {}

// newStore opens an embedded store in a temp directory and closes it when the
// test ends.
//
// Closing is not optional: the store holds its directory open, so t.TempDir's
// cleanup fails with "directory not empty" and marks the test failed even
// though every assertion passed. Registered AFTER t.TempDir so it runs first —
// cleanups run last-in-first-out.
func newStore(t *testing.T) repositories.KnowledgeGraphStore {
	t.Helper()

	store, err := oxigraph.NewEmbeddedStore(t.TempDir(), shapeTestLogger{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if closer, ok := store.(io.Closer); ok {
			_ = closer.Close()
		}
	})
	return store
}

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
	store := newStore(t)
	doc := json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","@type":"Meal"},
	  "@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"Dinner"}]}`)

	if err := store.LoadOntology(
		context.Background(), "application/ld+json", jsonld.InlineVocabContext(doc)); err == nil {
		t.Error("the store accepted @type inside a @context; if that ever becomes valid, " +
			"buildStorableContext's exclusion can be revisited — until then it is load-bearing")
	}
}

// TestRealTypeContextsStillLoad covers the shapes a resource type's @context
// actually carries beyond term mappings. WeOS reads them; JSON-LD does not
// accept them, and the store rejects the WHOLE document over any one of them —
// so the resource never reaches the knowledge graph while its API read and its
// projection stay healthy and report nothing.
//
// buildStorableContext must therefore copy term definitions only. These are the
// entries that broke it: `weos:abstract` and `weos:valueObject` (bools),
// `weos:adoptedTerms` (array) and `weos:termAliases` (object without `@id`)
// written by #513's adopt-term, and `rdfs:subClassOf`, whose value is a WeOS
// type slug rather than an IRI. `rdfs:subClassOf` is declared by commerce
// (Product, Offer, Order, Invoice, Refund, Expense…), finexity and
// mini-me-weos, so this is every resource of those types.
func TestRealTypeContextsStillLoad(t *testing.T) {
	for name, typeContext := range map[string]string{
		"abstract":     `"weos:abstract":true`,
		"valueObject":  `"weos:valueObject":true`,
		"adoptedTerms": `"weos:adoptedTerms":["recipe"]`,
		"termAliases":  `"weos:termAliases":{"recipe":["https://schema.org/isPartOf"]}`,
		"subClassOf":   `"rdfs:subClassOf":"economic-event"`,
		// The same control keys written the way someone who knows RDF would:
		// with a colon in the value. A value-shape test passes these straight
		// through, and the store rejects the document exactly as before.
		"subClassOfCompactIRI": `"rdfs:subClassOf":"schema:Thing"`,
		"subClassOfURN":        `"rdfs:subClassOf":"urn:type:agreement"`,
		"abstractString":       `"weos:abstract":"urn:flag:true"`,
	} {
		t.Run(name, func(t *testing.T) {
			full := json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Meal",` +
				`"recipe":{"@id":"https://schema.org/isPartOf","@type":"@id"},` + typeContext + `}`)
			doc := buildDocumentWith(t, full)

			store := newStore(t)
			if err := store.LoadOntology(
				context.Background(), "application/ld+json", jsonld.InlineVocabContext(doc)); err != nil {
				t.Fatalf("the store rejected a resource of a type declaring %s, so it never "+
					"reaches the knowledge graph at all: %v", name, err)
			}
			res, qErr := store.Query(context.Background(),
				`SELECT ?o WHERE { <urn:meal:1> <https://schema.org/isPartOf> ?o }`)
			if qErr != nil {
				t.Fatalf("query: %v", qErr)
			}
			if len(res.Bindings) != 1 {
				t.Errorf("the reference produced %d statements, want 1", len(res.Bindings))
			}
		})
	}
}

// buildDocumentWith runs a resource through the real write path so the stored
// @context is whatever buildStorableContext decided to keep.
func buildDocumentWith(t *testing.T, typeContext json.RawMessage) json.RawMessage {
	t.Helper()

	schema := json.RawMessage(`{"type":"object","properties":{
		"name":{"type":"string"},
		"recipe":{"type":"string","x-resource-type":"recipe"}}}`)
	refProps := application.ExtractReferenceProperties(schema, typeContext)
	doc, err := application.BuildResourceGraph(
		json.RawMessage(`{"name":"Dinner","recipe":"urn:recipe:A"}`),
		refProps, "urn:meal:1", "Meal", typeContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}
	return doc
}

// loadAndDescribe loads one document into a fresh store and returns its
// statements as a set, so two shapes can be compared regardless of order.
func loadAndDescribe(t *testing.T, doc json.RawMessage) map[string]bool {
	t.Helper()

	store := newStore(t)
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
