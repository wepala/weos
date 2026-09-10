//go:build oxigraph_embedded

package graph

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/graph/oxigraph"
)

// wm-fo2f9, against the real store: a resource projected as JSON-LD leaves a
// nested blank node and is pointed at by another resource; dropping it takes
// both, and leaves the other resource's own triples.
func TestSingleStores_DropAccountRemovesNestedNodesAndObjectTriples(t *testing.T) {
	store, err := oxigraph.NewEmbeddedStore(filepath.Join(t.TempDir(), "graph"), provLogger{})
	if err != nil {
		t.Fatalf("open embedded store: %v", err)
	}
	ctx := context.Background()
	doc := []byte(`{
	  "@context": {"@vocab": "https://schema.org/"},
	  "@graph": [
	    {"@id": "urn:recipe:harbor", "@type": "Recipe", "name": "Sunday Lasagna",
	     "nutrition": {"@type": "NutritionInformation", "calories": "450",
	                   "servingSize": {"@type": "QuantitativeValue", "value": "1"}}},
	    {"@id": "urn:recipe:cedar", "@type": "Recipe", "name": "Weeknight Dal", "isBasedOn": {"@id": "urn:recipe:harbor"}}
	  ]
	}`)
	if err := store.LoadOntology(ctx, "application/ld+json", doc); err != nil {
		t.Fatalf("load: %v", err)
	}
	ask := func(query string) bool {
		t.Helper()
		result, err := store.Query(ctx, query)
		if err != nil {
			t.Fatalf("query %s: %v", query, err)
		}
		return result.Boolean != nil && *result.Boolean
	}
	if !ask(`ASK { ?n <https://schema.org/calories> "450" }`) || !ask(`ASK { ?n <https://schema.org/value> "1" }`) {
		t.Fatal("the nested nodes were not projected; the test proves nothing")
	}
	if !ask(`ASK { <urn:recipe:cedar> <https://schema.org/isBasedOn> <urn:recipe:harbor> }`) {
		t.Fatal("the link to the resource was not projected; the test proves nothing")
	}

	stores := repositories.NewSingleKnowledgeGraphStores(store)
	if err := stores.DropAccount(ctx, "acct-harbor", []string{"urn:recipe:harbor"}); err != nil {
		t.Fatalf("DropAccount: %v", err)
	}
	if ask(`ASK { <urn:recipe:harbor> ?p ?o }`) {
		t.Error("the resource's own triples remain")
	}
	if ask(`ASK { ?n <https://schema.org/calories> "450" }`) || ask(`ASK { ?n <https://schema.org/value> "1" }`) {
		t.Error("a nested node of the deleted resource remains in the graph")
	}
	if ask(`ASK { ?s ?p <urn:recipe:harbor> }`) {
		t.Error("a triple pointing at the deleted resource remains")
	}
	if !ask(`ASK { <urn:recipe:cedar> <https://schema.org/name> "Weeknight Dal" }`) {
		t.Error("the other resource's own triples were taken")
	}
}
