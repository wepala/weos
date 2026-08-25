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
	"strings"
	"testing"
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
	}
	for name, c := range cases {
		if got := resourceTypeClassIRI(c.name, c.slug, json.RawMessage(c.ctx)); got != c.want {
			t.Errorf("%s: class IRI = %s, want %s", name, got, c.want)
		}
	}
}

func TestOntologyDocument_StripsWhatTheStoreRejects(t *testing.T) {
	raw := json.RawMessage(`{"@vocab":"https://schema.org/","@type":"foaf:Person","foaf":"http://xmlns.com/foaf/0.1/",
	  "weos:adoptedTerms":["@type"],"weos:termAliases":{"x":["y"]},"weos:abstract":false,"rdfs:subClassOf":"thing"}`)
	got := string(ontologyDocument(raw))
	for _, gone := range []string{"weos:adoptedTerms", "weos:termAliases", "weos:abstract", "rdfs:subClassOf"} {
		if strings.Contains(got, gone) {
			t.Errorf("ontology document still carries %s: %s", gone, got)
		}
	}
	for _, kept := range []string{`"@type":"foaf:Person"`, `"foaf":"http://xmlns.com/foaf/0.1/"`, `"@vocab"`} {
		if !strings.Contains(got, kept) {
			t.Errorf("ontology document lost %s: %s", kept, got)
		}
	}
}

func TestAdoptRemedy_NamesACommandThatAdoptsTheClass(t *testing.T) {
	if got := AdoptRemedy("core", "person", []string{"@type"}); !strings.Contains(got, "--term @type") ||
		strings.Contains(got, "--all") {
		t.Errorf("a held class alone must name --term @type, got %q", got)
	}
	if got := AdoptRemedy("core", "person", []string{"maker", "@type"}); !strings.Contains(got, "--all") ||
		!strings.Contains(got, "--term @type") {
		t.Errorf("a held class beside other terms needs both commands, got %q", got)
	}
	if got := AdoptRemedy("catalog", "widget", []string{"maker"}); got != "weos resource-type adopt-term catalog widget --all" {
		t.Errorf("plain held terms keep the sweep, got %q", got)
	}
}
