//go:build oxigraph_embedded

package oxigraph_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/wepala/weos/v3/infrastructure/graph/oxigraph"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

type nopLogger2 struct{}

func (nopLogger2) Debug(context.Context, string, ...interface{}) {}
func (nopLogger2) Info(context.Context, string, ...interface{})  {}
func (nopLogger2) Warn(context.Context, string, ...interface{})  {}
func (nopLogger2) Error(context.Context, string, ...interface{}) {}

func load(t *testing.T, name string, doc json.RawMessage) {
	t.Helper()
	store, err := oxigraph.NewEmbeddedStore(t.TempDir(), nopLogger2{})
	if err != nil {
		t.Fatalf("%s: open: %v", name, err)
	}
	if err := store.LoadOntology(context.Background(), "application/ld+json", jsonld.InlineVocabContext(doc)); err != nil {
		fmt.Printf("%-28s LOAD ERROR: %v\n", name, err)
		return
	}
	res, err := store.Query(context.Background(), "SELECT ?s ?p ?o WHERE { ?s ?p ?o }")
	if err != nil {
		t.Fatalf("%s: query: %v", name, err)
	}
	fmt.Printf("%-28s TRIPLES: %d\n", name, len(res.Bindings))
	for _, b := range res.Bindings {
		fmt.Printf("      %s  %s  %s\n", b["s"], b["p"], b["o"])
	}
}

// The context a resource now stores after #515: every term of the resource
// type's context is kept verbatim, including the weos:/rdfs: control entries.
func TestPrem515StoredContextShapes(t *testing.T) {
	load(t, "abstract-bool", json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","weos:abstract":true,"recipe":"https://schema.org/isPartOf"},
	  "@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"D"},{"@id":"urn:meal:1","recipe":{"@id":"urn:recipe:A"}}]}`))

	load(t, "valueObject-bool", json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","weos:valueObject":true,"recipe":"https://schema.org/isPartOf"},
	  "@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"D"},{"@id":"urn:meal:1","recipe":{"@id":"urn:recipe:A"}}]}`))

	load(t, "subClassOf-slug", json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","rdfs:subClassOf":"instrument","recipe":"https://schema.org/isPartOf"},
	  "@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"D"},{"@id":"urn:meal:1","recipe":{"@id":"urn:recipe:A"}}]}`))

	load(t, "termAliases-object", json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","weos:termAliases":{"recipe":["https://schema.org/old"]},"recipe":"https://schema.org/isPartOf"},
	  "@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"D"},{"@id":"urn:meal:1","recipe":{"@id":"urn:recipe:A"}}]}`))

	load(t, "adoptedTerms-array", json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","weos:adoptedTerms":["recipe"],"recipe":"https://schema.org/isPartOf"},
	  "@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"D"},{"@id":"urn:meal:1","recipe":{"@id":"urn:recipe:A"}}]}`))

	// Object-form term with @type:@id, the shape the ticket proposes.
	load(t, "objectform-term", json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","fileId":{"@id":"https://api.littleapollo.com/schema/associated","@type":"@id"}},
	  "@graph":[{"@id":"urn:q:1","@type":"Question","name":"D"},{"@id":"urn:q:1","fileId":{"@id":"urn:file:A"}}]}`))

	// Reference property with NO term: expands through @vocab.
	load(t, "vocab-only-edge", json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/"},
	  "@graph":[{"@id":"urn:q:1","@type":"Question","name":"D"},{"@id":"urn:q:1","fileId":{"@id":"urn:file:A"}}]}`))
}
