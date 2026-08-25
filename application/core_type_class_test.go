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

// Issue #521: Person and Organization declare a class, and the class the
// ontology projection advertises is the one resources carry. The e2e binding
// re-derives this IRI because resourceTypeClassIRI is unexported; this guard
// keeps the two from drifting.
func TestResourceTypeClassIRI_CoreTypes(t *testing.T) {
	cases := map[string]struct{ name, slug, ctx, want string }{
		"person": {"Person", "person",
			`{"@vocab":"https://schema.org/","foaf":"http://xmlns.com/foaf/0.1/","@type":"foaf:Person"}`,
			"http://xmlns.com/foaf/0.1/Person"},
		"organization": {"Organization", "organization",
			`{"@vocab":"https://schema.org/","org":"http://www.w3.org/ns/org#","@type":"org:Organization"}`,
			"http://www.w3.org/ns/org#Organization"},
		"undeclared falls back to the name through @vocab": {"Person", "person",
			`{"@vocab":"https://schema.org/","foaf":"http://xmlns.com/foaf/0.1/"}`,
			"https://schema.org/Person"},
		"no vocab falls back to a urn": {"Widget", "widget", `{"@type":"Widget"}`, "urn:type:widget"},
	}
	for name, c := range cases {
		if got := resourceTypeClassIRI(c.name, c.slug, json.RawMessage(c.ctx)); got != c.want {
			t.Errorf("%s: class IRI = %s, want %s", name, got, c.want)
		}
	}
}

func TestAdoptRemedy_NamesACommandThatAdoptsTheClass(t *testing.T) {
	if got := AdoptRemedy("core", "person", []string{"@type"}, nil); got != "weos resource-type adopt-term core person --term @type" {
		t.Errorf("a held class alone must name --term @type, got %q", got)
	}
	if got := AdoptRemedy("core", "person", []string{"maker", "@type"}, nil); got !=
		"weos resource-type adopt-term core person --term @type && weos resource-type adopt-term core person --all" {
		t.Errorf("a held class beside other terms needs both commands, copy-pasteable, got %q", got)
	}
	// A prefix the stored @type expands through moves the class too; a
	// sweep skips it, so the remedy names it and offers no sweep.
	if got := AdoptRemedy("core", "person", []string{"foaf"}, []string{"foaf"}); got !=
		"weos resource-type adopt-term core person --term foaf" {
		t.Errorf("a class-moving prefix must be named, got %q", got)
	}
	if AdoptRemedyNote([]string{"@type"}, nil) == "" || AdoptRemedyNote([]string{"maker"}, nil) != "" ||
		AdoptRemedyNote([]string{"foaf"}, []string{"foaf"}) == "" {
		t.Error("the note explains a moved class and nothing else")
	}
	if got := AdoptRemedy("catalog", "widget", []string{"maker"}, nil); got != "weos resource-type adopt-term catalog widget --all" {
		t.Errorf("plain held terms keep the sweep, got %q", got)
	}
	if got := AdoptRemedy("memory", "fact", []string{"@vocab"}, nil); got != "weos resource-type adopt-term memory fact --term @vocab" {
		t.Errorf("a held @vocab is named, never swept, got %q", got)
	}
	if AdoptRemedyNote([]string{"@vocab"}, nil) == "" {
		t.Error("the note explains a held @vocab")
	}
}

func TestAdoptTerms_RecordsNoAliasForAMovedClass(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Thing"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Product"}`)
	adopted, terms, err := adoptTerms(stored, preset, []movedPredicate{{Term: "@type", Property: "@type",
		StoredIRI: "https://schema.org/Thing", PresetIRI: "https://schema.org/Product"}})
	if err != nil || len(terms) != 1 {
		t.Fatalf("adopt: %v %v", terms, err)
	}
	if aliases := jsonld.TermAliases(adopted); len(aliases["@type"]) != 0 {
		t.Errorf("a class IRI must never enter the alias map: %v", aliases)
	}
}
