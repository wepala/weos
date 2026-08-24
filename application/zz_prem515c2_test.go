package application

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
)

// Legacy rows (written before #515) and rows that ended up holding both keys.
func TestPrem515CLegacyAndHalfMigrated(t *testing.T) {
	typeCtx := json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Meal","recipeId":"https://schema.org/isPartOf"}`)
	schema := json.RawMessage(`{"properties":{"recipeId":{"type":"string","x-resource-type":"recipe"}}}`)
	refProps := ExtractReferenceProperties(schema, typeCtx)
	pred := refProps[0].PredicateIRI

	docs := map[string]string{
		"legacy-expanded": `{"@context":{"@vocab":"https://schema.org/","recipeId":"https://schema.org/isPartOf"},
			"@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"D"},
			          {"@id":"urn:meal:1","https://schema.org/isPartOf":{"@id":"urn:recipe:A"}}]}`,
		"half-migrated-both": `{"@context":{"@vocab":"https://schema.org/","recipeId":"https://schema.org/isPartOf"},
			"@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"D"},
			          {"@id":"urn:meal:1","https://schema.org/isPartOf":{"@id":"urn:recipe:A"},"recipeId":{"@id":"urn:recipe:A"}}]}`,
		"legacy-stripped-ctx": `{"@context":"https://schema.org/",
			"@graph":[{"@id":"urn:meal:1","@type":"Meal","name":"D"},
			          {"@id":"urn:meal:1","https://schema.org/isPartOf":{"@id":"urn:recipe:A"}}]}`,
	}
	for name, raw := range docs {
		d := json.RawMessage(raw)
		fmt.Printf("\n=== %s ===\n", name)
		s, _ := entities.SimplifyJSONLD(d, typeCtx)
		fmt.Printf("read:      %s\n", s)
		fmt.Printf("triples:   %v\n", ExtractTriplesFromData(refProps, d, "urn:meal:1"))
		fmt.Printf("edgeValues:%v\n", EdgeValues(d, typeCtx, "recipeId"))
		add, err := AddEdgeToGraph(d, pred, "urn:recipe:B", "urn:meal:1")
		fmt.Printf("add B:     %s err=%v\n", add, err)
		rm, err := RemoveEdgeFromGraph(d, pred, "urn:recipe:A")
		fmt.Printf("remove A:  %s err=%v\n", rm, err)
		s2, _ := entities.SimplifyJSONLD(rm, typeCtx)
		fmt.Printf("read after remove: %s\n", s2)
	}
}

// Predicate that is not an http/https/urn IRI: a term whose prefix the
// context never defines, with no @vocab to fall back on.
func TestPrem515CPrefixFormPredicate(t *testing.T) {
	typeCtx := json.RawMessage(`{"@type":"Person","knows":"foaf:knows"}`)
	schema := json.RawMessage(`{"properties":{"knows":{"type":"string","x-resource-type":"person"}}}`)
	refProps := ExtractReferenceProperties(schema, typeCtx)
	fmt.Printf("\npredicate resolved to: %q\n", refProps[0].PredicateIRI)
	legacy := json.RawMessage(`{"@context":{"knows":"foaf:knows"},
		"@graph":[{"@id":"urn:person:1","@type":"Person","name":"A"},
		          {"@id":"urn:person:1","foaf:knows":{"@id":"urn:person:2"}}]}`)
	s, _ := entities.SimplifyJSONLD(legacy, typeCtx)
	fmt.Printf("legacy read: %s\n", s)
	row := map[string]any{}
	fmt.Printf("edgeValues: %v\n", EdgeValues(legacy, typeCtx, "knows"))
	_ = row
}

// A property whose NAME looks like an IRI.
func TestPrem515CIRIShapedPropertyName(t *testing.T) {
	typeCtx := json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Thing"}`)
	schema := json.RawMessage(`{"properties":{"https://x.test/rel":{"type":"string","x-resource-type":"thing"}}}`)
	refProps := ExtractReferenceProperties(schema, typeCtx)
	flat := json.RawMessage(`{"name":"T","https://x.test/rel":"urn:thing:2"}`)
	g, err := BuildResourceGraph(flat, refProps, "urn:thing:1", "Thing", typeCtx)
	fmt.Printf("\npredicate=%q\nstored: %s err=%v\n", refProps[0].PredicateIRI, g, err)
	s, _ := entities.SimplifyJSONLD(g, typeCtx)
	fmt.Printf("read back: %s\n", s)
}
