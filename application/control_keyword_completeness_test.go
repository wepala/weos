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
	"encoding/json"
	"testing"

	"github.com/wepala/weos/v3/pkg/jsonld"
)

// Issue #522: the boot's completeness check walks the reverse map to decide
// whether a reference property's writes would be dropped. A control keyword
// that expanded onto the property's predicate could steal the reverse entry
// and make a healthy property look uncovered.
func TestReferencePropertiesWithoutContextEntry_IgnoresAControlKeyword(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{
	  "maker":{"type":"string","x-resource-type":"vendor"}}}`)
	ctx := json.RawMessage(`{"@vocab":"https://schema.org/",
	  "maker":{"@id":"https://example.org/catalog#madeBy","@type":"@id"},
	  "rdfs:subClassOf":"https://example.org/catalog#madeBy"}`)
	// Repeated: before the fix the outcome was map-iteration order.
	for i := 0; i < 100; i++ {
		if dropped := referencePropertiesWithoutContextEntry(schema, ctx); len(dropped) != 0 {
			t.Fatalf("run %d: maker is covered by its own term; reported as dropped: %v", i, dropped)
		}
	}
}

// The boot never HOLDS a control entry: a stored rdfs:subClassOf whose value
// expands through a prefix the preset adds is not a predicate moving, so the
// prefix is adopted and nothing is held.
func TestHoldMovingTerms_ControlEntryIsNotAPredicate(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"cat:Gadget",
	  "maker":{"@id":"https://schema.org/maker","@type":"@id"}}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","cat":"https://example.org/catalog#",
	  "maker":{"@id":"https://schema.org/maker","@type":"@id"}}`)
	schema := json.RawMessage(`{"type":"object","properties":{"maker":{"type":"string","x-resource-type":"vendor"}}}`)
	rec, err := reconcileAdditiveContext(stored, preset, schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Moves) != 0 || len(rec.Added) != 1 || rec.Added[0] != "cat" {
		t.Fatalf("the cat prefix must be added, not held: %+v", rec)
	}
}

// The boot reconcile treats a control entry the way ParseContext now does:
// it merges by copy and is never held as a moving term or flagged for an
// undefined prefix, even when its value looks like a compact IRI.
func TestReconcileAdditiveContext_ControlEntryMergesByCopy(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","maker":{"@id":"https://schema.org/maker","@type":"@id"}}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","maker":{"@id":"https://schema.org/maker","@type":"@id"},
	  "rdfs:subClassOf":"schema:Thing","weos:abstract":true}`)
	schema := json.RawMessage(`{"type":"object","properties":{"maker":{"type":"string","x-resource-type":"vendor"}}}`)
	rec, err := reconcileAdditiveContext(stored, preset, schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Moves) != 0 || len(rec.Conflicts) != 0 || !rec.Changed {
		t.Fatalf("control entries must merge by copy: %+v", rec)
	}
	if got := jsonld.SubClassOf(rec.Context); got != "schema:Thing" {
		t.Errorf("rdfs:subClassOf not merged: %q (context %s)", got, rec.Context)
	}
	if !jsonld.IsAbstract(rec.Context) {
		t.Errorf("weos:abstract not merged: %s", rec.Context)
	}
}

// A legacy document whose embedded @context still carries rdfs:subClassOf on
// a property's predicate: a new Triple.Created edge must land under the
// property name, never under the control keyword (which sorted first).
func TestAddEdgeToGraph_ControlKeywordNeverNamesTheEdge(t *testing.T) {
	doc := json.RawMessage(`{"@context":{"@vocab":"https://schema.org/","rdfs:subClassOf":"supplier",
	  "supplier":{"@id":"https://schema.org/supplier","@type":"@id"}},
	  "@graph":[{"@id":"urn:widget:1","@type":"Widget"}]}`)
	for i := 0; i < 20; i++ {
		out, err := AddEdgeToGraph(doc, "https://schema.org/supplier", "urn:vendor:1", "urn:widget:1")
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		edges, _ := m["@graph"].([]any)[1].(map[string]any)
		if _, wrong := edges["rdfs:subClassOf"]; wrong || edges["supplier"] == nil {
			t.Fatalf("run %d: the edge landed under %v", i, edges)
		}
	}
}
