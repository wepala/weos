package application

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
)

func TestPrem515CVocabDerivedEdgeDuplicate(t *testing.T) {
	cases := []struct {
		name   string
		ctx    string
		schema string
	}{
		{"vocab-only-context", `{"@vocab":"https://schema.org/","@type":"Meal"}`,
			`{"properties":{"recipeId":{"type":"string","x-resource-type":"recipe"}}}`},
		{"context-with-other-term", `{"@vocab":"https://schema.org/","@type":"Meal","note":"https://schema.org/comment"}`,
			`{"properties":{"recipeId":{"type":"string","x-resource-type":"recipe"}}}`},
		{"context-with-term-for-this-prop", `{"@vocab":"https://schema.org/","@type":"Meal","recipeId":"https://schema.org/isPartOf"}`,
			`{"properties":{"recipeId":{"type":"string","x-resource-type":"recipe"}}}`},
	}
	for _, tc := range cases {
		ctx := json.RawMessage(tc.ctx)
		refProps := ExtractReferenceProperties(json.RawMessage(tc.schema), ctx)
		flat := json.RawMessage(`{"name":"Dinner","recipeId":"urn:recipe:A"}`)
		graph, err := BuildResourceGraph(flat, refProps, "urn:meal:1", "Meal", ctx)
		if err != nil {
			t.Fatalf("%s: build: %v", tc.name, err)
		}
		fmt.Printf("\n=== %s ===\npredicate: %s\nstored:  %s\n", tc.name, refProps[0].PredicateIRI, graph)

		// Replay of the Triple.Created event that Create emits alongside it.
		after, err := AddEdgeToGraph(graph, refProps[0].PredicateIRI, "urn:recipe:A", "urn:meal:1")
		if err != nil {
			t.Fatalf("%s: add: %v", tc.name, err)
		}
		fmt.Printf("replayed: %s\n", after)

		// What every reader then sees.
		simple, _ := entities.SimplifyJSONLD(after, ctx)
		fmt.Printf("simplified: %s\n", simple)
		trips := ExtractTriplesFromData(refProps, after, "urn:meal:1")
		fmt.Printf("triples: %v\n", trips)

		// And what a later delete of that reference does.
		removed, rerr := RemoveEdgeFromGraph(after, refProps[0].PredicateIRI, "urn:recipe:A")
		fmt.Printf("after remove: %s err=%v\n", removed, rerr)
		s2, _ := entities.SimplifyJSONLD(removed, ctx)
		fmt.Printf("simplified after remove: %s\n", s2)
	}
}
