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
)

func edgeKeyTestResolver(t *testing.T, ldContext, schema string) *edgeKeyResolver {
	t.Helper()
	return newEdgeKeyResolver(json.RawMessage(ldContext), json.RawMessage(schema), nil)
}

func decodeDoc(t *testing.T, raw string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("bad test document: %v", err)
	}
	return doc
}

func edgesOf(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	graph, _ := doc["@graph"].([]any)
	if len(graph) < 2 {
		t.Fatalf("document has no edges node: %v", doc)
	}
	edges, _ := graph[1].(map[string]any)
	return edges
}

const widgetSchema = `{"type":"object","properties":{
  "name":{"type":"string"},
  "maker":{"type":"string","x-resource-type":"vendor"},
  "partner":{"type":"string","x-resource-type":"widget"},
  "supplier":{"type":"string","x-resource-type":"vendor"}}}`

func TestEdgeKeyResolver_ResolvesLikeTheReadPath(t *testing.T) {
	r := edgeKeyTestResolver(t, `{"@vocab":"https://schema.org/",
	  "maker":{"@id":"https://example.org/catalog#madeBy","@type":"@id"},
	  "weos:termAliases":{"maker":["https://schema.org/maker"]}}`, widgetSchema)

	cases := map[string]string{
		"maker":                              "maker",    // already a property name
		"https://example.org/catalog#madeBy": "maker",    // live term
		"https://schema.org/maker":           "maker",    // alias
		"https://schema.org/supplier":        "supplier", // @vocab prefix, no term (#510)
	}
	for key, want := range cases {
		name, candidates, ok := r.resolve(key)
		if !ok || name != want || candidates != nil {
			t.Errorf("resolve(%q) = (%q, %v, %v), want (%q, nil, true)", key, name, candidates, ok, want)
		}
	}
	if _, _, ok := r.resolve("https://example.org/legacy#madeBy"); ok {
		t.Error("an IRI nothing names must not resolve")
	}
}

func TestEdgeKeyResolver_AmbiguityIsDetectedForward(t *testing.T) {
	r := edgeKeyTestResolver(t, `{"@vocab":"https://schema.org/",
	  "maker":{"@id":"https://schema.org/associated","@type":"@id"},
	  "partner":{"@id":"https://schema.org/associated","@type":"@id"}}`, widgetSchema)
	name, candidates, ok := r.resolve("https://schema.org/associated")
	if ok || name != "" {
		t.Fatalf("a shared predicate must not resolve; got %q", name)
	}
	if len(candidates) != 2 || candidates[0] != "maker" || candidates[1] != "partner" {
		t.Fatalf("candidates = %v, want [maker partner]", candidates)
	}
}

func TestEdgeKeyResolver_NilMeansUnresolved(t *testing.T) {
	var r *edgeKeyResolver
	if name, _, ok := r.resolve("maker"); !ok || name != "maker" {
		t.Error("a property-name key needs no resolver")
	}
	if _, _, ok := r.resolve("https://schema.org/maker"); ok {
		t.Error("an IRI key with no type context must be unresolved")
	}
}

func TestNormalizeEdgeKeys_RewritesAndEmbedsTheStorableContext(t *testing.T) {
	r := edgeKeyTestResolver(t, `{"@vocab":"https://schema.org/",
	  "maker":{"@id":"https://schema.org/maker","@type":"@id"},"weos:abstract":false}`, widgetSchema)
	doc := decodeDoc(t, `{"@context":"https://schema.org/","@graph":[
	  {"@id":"urn:widget:1","@type":"Widget","name":"Bolt cutter"},
	  {"@id":"urn:widget:1","https://schema.org/maker":{"@id":"urn:vendor:1"},
	   "https://schema.org/supplier":[{"@id":"urn:vendor:2"}]}]}`)
	entityBefore, _ := json.Marshal(doc["@graph"].([]any)[0])

	changed, problems := normalizeEdgeKeys(doc, r)
	if !changed || len(problems) != 0 {
		t.Fatalf("changed=%v problems=%v", changed, problems)
	}
	edges := edgesOf(t, doc)
	if _, ok := edges["maker"]; !ok {
		t.Errorf("maker edge not rewritten: %v", edges)
	}
	if list, ok := edges["supplier"].([]any); !ok || len(list) != 1 {
		t.Errorf("supplier list edge not rewritten as a list: %v", edges)
	}
	for key := range edges {
		if key != "@id" && key != "maker" && key != "supplier" {
			t.Errorf("unexpected edge key %q left behind", key)
		}
	}
	entityAfter, _ := json.Marshal(doc["@graph"].([]any)[0])
	if string(entityBefore) != string(entityAfter) {
		t.Errorf("entity node changed: %s -> %s", entityBefore, entityAfter)
	}
	ctx, _ := doc["@context"].(map[string]any)
	if ctx["@vocab"] != "https://schema.org/" || ctx["maker"] == nil {
		t.Errorf("document @context is not the storable context: %v", doc["@context"])
	}
	if _, leaked := ctx["weos:abstract"]; leaked {
		t.Error("control keyword leaked into the document @context")
	}
}

func TestNormalizeEdgeKeys_ReportsWithoutRewriting(t *testing.T) {
	r := edgeKeyTestResolver(t, `{"@vocab":"https://schema.org/",
	  "maker":{"@id":"https://schema.org/associated","@type":"@id"},
	  "partner":{"@id":"https://schema.org/associated","@type":"@id"}}`, widgetSchema)
	doc := decodeDoc(t, `{"@context":"https://schema.org/","@graph":[
	  {"@id":"urn:widget:1","@type":"Widget"},
	  {"@id":"urn:widget:1","https://schema.org/associated":{"@id":"urn:vendor:1"},
	   "https://example.org/legacy#x":{"@id":"urn:vendor:1"},
	   "https://schema.org/supplier":{"@id":"urn:vendor:1"}}]}`)
	changed, problems := normalizeEdgeKeys(doc, r)
	if !changed {
		t.Fatal("the resolvable supplier edge should still be rewritten")
	}
	if len(problems) != 2 {
		t.Fatalf("problems = %v, want an ambiguous and an unresolved one", problems)
	}
	edges := edgesOf(t, doc)
	if _, kept := edges["https://schema.org/associated"]; !kept {
		t.Error("the ambiguous edge must be left keyed by its IRI")
	}
	if _, kept := edges["https://example.org/legacy#x"]; !kept {
		t.Error("the unresolved edge must be left keyed by its IRI")
	}
	if _, ok := edges["supplier"]; !ok {
		t.Error("the resolvable edge beside them must be rewritten")
	}
	var ambiguous, unresolved int
	for _, p := range problems {
		if len(p.Candidates) > 1 {
			ambiguous++
		} else {
			unresolved++
		}
	}
	if ambiguous != 1 || unresolved != 1 {
		t.Errorf("ambiguous=%d unresolved=%d", ambiguous, unresolved)
	}
}

func TestNormalizeEdgeKeys_IsANoOpOnCompactDocuments(t *testing.T) {
	r := edgeKeyTestResolver(t, `{"@vocab":"https://schema.org/"}`, widgetSchema)
	doc := decodeDoc(t, `{"@context":"https://schema.org/","@graph":[
	  {"@id":"urn:widget:1","@type":"Widget"},{"@id":"urn:widget:1","maker":{"@id":"urn:vendor:1"}}]}`)
	before, _ := json.Marshal(doc)
	changed, problems := normalizeEdgeKeys(doc, r)
	after, _ := json.Marshal(doc)
	if changed || len(problems) != 0 || string(before) != string(after) {
		t.Errorf("compact document was touched: changed=%v problems=%v\n%s\n%s", changed, problems, before, after)
	}
	if changed, _ := normalizeEdgeKeys(decodeDoc(t, `{"@graph":[{"@id":"urn:widget:1"}]}`), r); changed {
		t.Error("a document with no edges node must not change")
	}
}

func TestNormalizeEdgeKeys_DoesNotMergeWhenBothFormsArePresent(t *testing.T) {
	r := edgeKeyTestResolver(t, `{"@vocab":"https://schema.org/"}`, widgetSchema)
	doc := decodeDoc(t, `{"@graph":[{"@id":"urn:widget:1"},
	  {"@id":"urn:widget:1","maker":{"@id":"urn:vendor:1"},"https://schema.org/maker":{"@id":"urn:vendor:2"}}]}`)
	changed, problems := normalizeEdgeKeys(doc, r)
	if changed || len(problems) != 1 {
		t.Fatalf("changed=%v problems=%v; both forms present must be reported, never merged", changed, problems)
	}
}

func TestEdgeKeyResolver_SharedAliasIsAmbiguous(t *testing.T) {
	r := edgeKeyTestResolver(t, `{"@vocab":"https://schema.org/",
	  "maker":{"@id":"https://example.org/catalog#madeBy","@type":"@id"},
	  "partner":{"@id":"https://example.org/catalog#partneredWith","@type":"@id"},
	  "weos:termAliases":{"maker":["https://example.org/old#rel"],"partner":["https://example.org/old#rel"]}}`,
		widgetSchema)
	name, candidates, ok := r.resolve("https://example.org/old#rel")
	if ok || name != "" || len(candidates) != 2 {
		t.Fatalf("a historical IRI two properties recorded must be ambiguous; got (%q, %v, %v)", name, candidates, ok)
	}
}
