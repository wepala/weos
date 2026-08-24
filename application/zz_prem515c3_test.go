package application

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestPrem515CEdgeKeyInDirect(t *testing.T) {
	raw := json.RawMessage(`{"@context":{"@vocab":"https://schema.org/","recipeId":"https://schema.org/isPartOf"},
		"@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"D"},
		          {"@id":"urn:meal:1","https://schema.org/isPartOf":{"@id":"urn:recipe:A"},"recipeId":{"@id":"urn:recipe:A"}}]}`)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	g := doc["@graph"].([]any)
	edges := g[1].(map[string]any)
	k, shared := edgeKeyIn(edges, doc, "https://schema.org/isPartOf")
	fmt.Printf("edgeKeyIn -> %q shared=%v\n", k, shared)
	fmt.Printf("propertiesForPredicate -> %v\n", propertiesForPredicate(doc, "https://schema.org/isPartOf"))
	out, err := RemoveEdgeFromGraph(raw, "https://schema.org/isPartOf", "urn:recipe:A")
	fmt.Printf("remove -> %s err=%v\n", out, err)
}
