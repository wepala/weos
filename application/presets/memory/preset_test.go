package memory

import (
	"encoding/json"
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

func factType(t *testing.T) application.PresetResourceType {
	t.Helper()
	registry := application.NewPresetRegistry()
	Register(registry)
	preset, ok := registry.Get("memory")
	if !ok {
		t.Fatal("memory preset not registered")
	}
	for _, pt := range preset.Types {
		if pt.Slug == "fact" {
			return pt
		}
	}
	t.Fatal("fact type not found in memory preset")
	return application.PresetResourceType{}
}

func TestRegister_FactPresetShape(t *testing.T) {
	t.Parallel()

	fact := factType(t)
	var ctx, schema map[string]any
	if err := json.Unmarshal(fact.Context, &ctx); err != nil {
		t.Fatalf("fact context is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(fact.Schema, &schema); err != nil {
		t.Fatalf("fact schema is not valid JSON: %v", err)
	}
	if ctx["@type"] != "mem:Fact" {
		t.Errorf("fact @type = %v, want mem:Fact", ctx["@type"])
	}
	if _, declared := ctx["rdfs:subClassOf"]; declared {
		t.Error("fact context must not declare rdfs:subClassOf — projection treats it as a parent type slug")
	}
}

func TestFactClassIRI_ExpandsViaMemPrefix(t *testing.T) {
	t.Parallel()

	fact := factType(t)
	var rawCtx map[string]any
	if err := json.Unmarshal(fact.Context, &rawCtx); err != nil {
		t.Fatalf("unmarshal context: %v", err)
	}
	vocab, _ := jsonld.ParseContext(fact.Context)
	got := jsonld.ExpandIRI("mem:Fact", vocab, rawCtx)
	want := "https://weos.org/vocab/memory#Fact"
	if got != want {
		t.Errorf("class IRI = %q, want %q", got, want)
	}
}

func TestFactReferenceProperties_WasRevisionOfIsSelfReference(t *testing.T) {
	t.Parallel()

	fact := factType(t)
	refProps := application.ExtractReferenceProperties(fact.Schema, fact.Context)
	if len(refProps) != 1 {
		t.Fatalf("expected exactly one reference property, got %d: %+v", len(refProps), refProps)
	}
	rp := refProps[0]
	if rp.PropertyName != "wasRevisionOf" {
		t.Errorf("reference property = %q, want wasRevisionOf", rp.PropertyName)
	}
	if rp.PredicateIRI != "http://www.w3.org/ns/prov#wasRevisionOf" {
		t.Errorf("predicate IRI = %q, want prov:wasRevisionOf full IRI", rp.PredicateIRI)
	}
	if rp.TargetType != "fact" {
		t.Errorf("target type = %q, want fact (self-reference)", rp.TargetType)
	}
}

func TestFactGraph_SupersessionFlowsThroughPipeline(t *testing.T) {
	t.Parallel()

	fact := factType(t)
	refProps := application.ExtractReferenceProperties(fact.Schema, fact.Context)
	data := json.RawMessage(`{
		"statement": "Akeem prefers PRs based on the v3 branch",
		"about": "urn:person:akeem",
		"confidence": 0.9,
		"wasDerivedFrom": ["urn:event:2n8f3kdap0nWJXNKQqZfyz9O1Bd", "urn:event:2n8f3keXQ0aBcDeFgHiJkLmNoPq"],
		"wasRevisionOf": "urn:fact:2n8f3kOldFactKsuid0000000000",
		"invalidatedAtTime": "2026-07-02T00:00:00Z"
	}`)

	graph, err := application.BuildResourceGraph(data, refProps, "urn:fact:new", "Fact", fact.Context)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	// wasRevisionOf must land in the edges node as an object reference.
	if got := application.EdgeValue(graph, fact.Context, "wasRevisionOf"); got != "urn:fact:2n8f3kOldFactKsuid0000000000" {
		t.Errorf("wasRevisionOf edge = %q, want the prior fact URN", got)
	}

	// Provenance and supersession literals must stay on the entity node.
	var entity map[string]any
	if err := json.Unmarshal(application.ExtractEntityNode(graph), &entity); err != nil {
		t.Fatalf("unmarshal entity node: %v", err)
	}
	if entity["@type"] != "mem:Fact" {
		t.Errorf("entity @type = %v, want mem:Fact", entity["@type"])
	}
	derived, ok := entity["wasDerivedFrom"].([]any)
	if !ok || len(derived) != 2 {
		t.Fatalf("wasDerivedFrom = %v, want array of two source event URNs", entity["wasDerivedFrom"])
	}
	if entity["invalidatedAtTime"] != "2026-07-02T00:00:00Z" {
		t.Errorf("invalidatedAtTime = %v, want the supersession timestamp", entity["invalidatedAtTime"])
	}
	if _, inEntity := entity["wasRevisionOf"]; inEntity {
		t.Error("wasRevisionOf must not appear on the entity node — it is a reference edge")
	}

	// The storable @context must keep the PROV-O mappings for the literal
	// terms (bare full-IRI strings survive context stripping) so the graph
	// store expands them to the right predicates.
	var doc map[string]any
	if err := json.Unmarshal(graph, &doc); err != nil {
		t.Fatalf("unmarshal graph: %v", err)
	}
	storable, ok := doc["@context"].(map[string]any)
	if !ok {
		t.Fatalf("graph @context missing or not an object: %v", doc["@context"])
	}
	for term, want := range map[string]string{
		"wasDerivedFrom":    "http://www.w3.org/ns/prov#wasDerivedFrom",
		"invalidatedAtTime": "http://www.w3.org/ns/prov#invalidatedAtTime",
		"generatedAtTime":   "http://www.w3.org/ns/prov#generatedAtTime",
	} {
		if got := storable[term]; got != want {
			t.Errorf("storable context %q = %v, want %q", term, got, want)
		}
	}
}
