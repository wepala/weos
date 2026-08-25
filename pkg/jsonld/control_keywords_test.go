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

package jsonld

import (
	"encoding/json"
	"testing"
)

// Issue #522: a control keyword in a @context is control data, never a term.

const controlLadenContext = `{"@vocab":"https://schema.org/",
  "maker":{"@id":"https://schema.org/maker","@type":"@id"},
  "rdfs:subClassOf":"maker",
  "weos:abstract":false,
  "weos:valueObject":true,
  "weos:adoptedTerms":["maker"],
  "weos:termAliases":{"maker":["https://example.org/old#madeBy"]}}`

func TestParseContext_SkipsEveryControlKeyword(t *testing.T) {
	for name, ctx := range map[string]string{
		"string values": controlLadenContext,
		"@id object":    `{"@vocab":"https://schema.org/","maker":"https://schema.org/maker","rdfs:subClassOf":{"@id":"maker"}}`,
		"compact IRI":   `{"@vocab":"https://schema.org/","maker":"https://schema.org/maker","rdfs:subClassOf":"schema:maker"}`,
		"absolute IRI":  `{"@vocab":"https://schema.org/","maker":"https://schema.org/maker","rdfs:subClassOf":"https://schema.org/maker"}`,
	} {
		_, terms := ParseContext(json.RawMessage(ctx))
		for key := range ControlKeywords {
			if _, present := terms[key]; present {
				t.Errorf("%s: %s entered the term map as %q", name, key, terms[key])
			}
		}
		if terms["maker"] != "https://schema.org/maker" {
			t.Errorf("%s: the real term is gone: %v", name, terms)
		}
	}
}

// The skip is by NAME. A colon in a key is not the rule: a compact-IRI term
// such as "schema:knows" is a legitimate term definition and must stay.
func TestParseContext_SkipsByNameNotByColon(t *testing.T) {
	_, terms := ParseContext(json.RawMessage(`{"@vocab":"https://schema.org/",
	  "foaf":"http://xmlns.com/foaf/0.1/","foaf:knows":"http://xmlns.com/foaf/0.1/knows",
	  "rdfs:subClassOf":"thing"}`))
	if terms["foaf:knows"] != "http://xmlns.com/foaf/0.1/knows" {
		t.Errorf("a compact-IRI term must survive: %v", terms)
	}
	if _, present := terms["rdfs:subClassOf"]; present {
		t.Errorf("rdfs:subClassOf must not: %v", terms)
	}
}

func TestBuildReverseMap_ControlKeywordNeverClaimsAPredicate(t *testing.T) {
	// rdfs:subClassOf "maker" expands to the same IRI the maker term owns;
	// before the fix the winner was map-iteration order.
	for i := 0; i < 100; i++ {
		reverse := BuildReverseMap(json.RawMessage(controlLadenContext))
		if got := reverse["https://schema.org/maker"]; got != "maker" {
			t.Fatalf("run %d: https://schema.org/maker reverse-maps to %q, want maker", i, got)
		}
		if got := reverse["https://example.org/old#madeBy"]; got != "maker" {
			t.Fatalf("run %d: the alias must still resolve to maker, got %q", i, got)
		}
	}
}

func TestControlKeywordReadersDoNotUseTheTermMap(t *testing.T) {
	raw := json.RawMessage(controlLadenContext)
	if got := SubClassOf(raw); got != "maker" {
		t.Errorf("SubClassOf = %q", got)
	}
	if IsAbstract(raw) {
		t.Error("IsAbstract must read the raw false")
	}
	if !IsValueObject(raw) {
		t.Error("IsValueObject must read the raw true")
	}
	if got := AdoptedTerms(raw); len(got) != 1 || got[0] != "maker" {
		t.Errorf("AdoptedTerms = %v", got)
	}
	if got := TermAliases(raw)["maker"]; len(got) != 1 || got[0] != "https://example.org/old#madeBy" {
		t.Errorf("TermAliases = %v", TermAliases(raw))
	}
}

func TestEdgeProperty_ControlKeywordNeverClaimsALegacyKey(t *testing.T) {
	name, ok := EdgeProperty("https://schema.org/maker", json.RawMessage(controlLadenContext))
	if !ok || name != "maker" {
		t.Errorf("a legacy IRI key resolves to the property, got (%q, %v)", name, ok)
	}
	// A control keyword whose value collides with nothing does not invent a
	// property either: an IRI off @vocab that only rdfs:subClassOf named is
	// unresolved. (Under @vocab the read path's #510 fallback would name the
	// local part, which is that fallback's own contract, not the keyword's.)
	if _, ok := EdgeProperty("https://example.org/x#thing", json.RawMessage(
		`{"@vocab":"https://schema.org/","rdfs:subClassOf":"https://example.org/x#thing"}`)); ok {
		t.Error("rdfs:subClassOf must not resolve an edge key")
	}
}
