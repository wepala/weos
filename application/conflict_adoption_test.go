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

// Issue #518: a REDEFINED term (a Conflict) is reported and adoptable like an
// ADDED one, recording the IRI its edges are keyed by.

const widgetRefSchema = `{"type":"object","properties":{"name":{"type":"string"},
  "maker":{"type":"string","x-resource-type":"vendor"},"supplier":{"type":"string","x-resource-type":"vendor"}}}`

func TestConflictMoves_ATermMovesItself(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","maker":{"@id":"https://schema.org/maker","@type":"@id"}}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","maker":{"@id":"https://example.org/catalog#madeBy","@type":"@id"}}`)
	moves := conflictMoves(stored, preset, json.RawMessage(widgetRefSchema), []string{"maker"})
	if len(moves) != 1 || moves[0].Property != "maker" || moves[0].StoredIRI != "https://schema.org/maker" ||
		moves[0].PresetIRI != "https://example.org/catalog#madeBy" {
		t.Fatalf("moves = %+v", moves)
	}
}

func TestConflictMoves_APrefixMovesEveryPropertyThroughItAndNeverItself(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","cat":"https://schema.org/",
	  "maker":"cat:madeBy","supplier":"cat:suppliedBy","@type":"cat:Widget"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","cat":"https://example.org/catalog#",
	  "maker":"cat:madeBy","supplier":"cat:suppliedBy","@type":"cat:Widget"}`)
	moves := conflictMoves(stored, preset, json.RawMessage(widgetRefSchema), []string{"cat"})
	got := map[string]string{}
	for _, m := range moves {
		if m.Term != "cat" {
			t.Fatalf("every move belongs to the prefix: %+v", moves)
		}
		got[m.Property] = m.StoredIRI + " -> " + m.PresetIRI
	}
	for property, want := range map[string]string{
		"maker":    "https://schema.org/madeBy -> https://example.org/catalog#madeBy",
		"supplier": "https://schema.org/suppliedBy -> https://example.org/catalog#suppliedBy",
		"@type":    "https://schema.org/Widget -> https://example.org/catalog#Widget",
	} {
		if got[property] != want {
			t.Errorf("%s: %q, want %q (all: %v)", property, got[property], want, got)
		}
	}
	if _, self := got["cat"]; self {
		t.Error("the prefix must never list itself as a moved property")
	}
}

func TestConflictMoves_VocabMovesEveryUntermedReference(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","maker":{"@id":"https://schema.org/maker","@type":"@id"}}`)
	preset := json.RawMessage(`{"@vocab":"https://example.org/catalog#","maker":{"@id":"https://schema.org/maker","@type":"@id"}}`)
	moves := conflictMoves(stored, preset, json.RawMessage(widgetRefSchema), []string{"@vocab"})
	if len(moves) != 1 || moves[0].Property != "supplier" || moves[0].StoredIRI != "https://schema.org/supplier" ||
		moves[0].PresetIRI != "https://example.org/catalog#supplier" {
		t.Fatalf("@vocab moves the untermed supplier and nothing else: %+v", moves)
	}
}

func TestSelectTermsToAdopt_SweepRules(t *testing.T) {
	held := []movedPredicate{
		{Term: "maker", Property: "maker", StoredIRI: "a", PresetIRI: "b"},
		{Term: "cat", Property: "supplier", StoredIRI: "c", PresetIRI: "d"},
		{Term: "cat", Property: "@type", StoredIRI: "C", PresetIRI: "D"},
		{Term: "@vocab", Property: "distributor", StoredIRI: "e", PresetIRI: "f"},
		{Term: "@type", Property: "@type", StoredIRI: "T", PresetIRI: "U"},
	}
	selected, stillHeld, err := selectTermsToAdopt(held, nil, nil, nil, "widget")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Term != "maker" {
		t.Errorf("a sweep takes only the plain term: %+v", selected)
	}
	if got := stillHeld; len(got) != 3 || got[0] != "@type" || got[1] != "@vocab" || got[2] != "cat" {
		t.Errorf("stillHeld = %v", got)
	}
	named, _, err := selectTermsToAdopt(held, []string{"cat"}, nil, nil, "widget")
	if err != nil || len(named) != 2 {
		t.Errorf("naming the prefix takes every move under it: %+v %v", named, err)
	}
	if _, _, err := selectTermsToAdopt(held, []string{"nothing"}, json.RawMessage(`{}`), json.RawMessage(`{}`), "widget"); err == nil {
		t.Error("a term the boot never held is refused")
	}
}

func TestAdoptTerms_AConflictPrefixAliasesEveryPropertyItMoves(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","cat":"https://schema.org/",
	  "maker":"cat:madeBy","supplier":"cat:suppliedBy"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","cat":"https://example.org/catalog#",
	  "maker":"cat:madeBy","supplier":"cat:suppliedBy"}`)
	moves := conflictMoves(stored, preset, json.RawMessage(widgetRefSchema), []string{"cat"})
	adopted, terms, err := adoptTerms(stored, preset, moves)
	if err != nil || len(terms) != 1 || terms[0] != "cat" {
		t.Fatalf("adopted %v %v", terms, err)
	}
	aliases := jsonld.TermAliases(adopted)
	if aliases["maker"][0] != "https://schema.org/madeBy" || aliases["supplier"][0] != "https://schema.org/suppliedBy" {
		t.Errorf("aliases = %v", aliases)
	}
	if _, self := aliases["cat"]; self {
		t.Error("no alias is recorded against the prefix itself")
	}
	if again, terms2, _ := adoptTerms(adopted, preset, conflictMoves(adopted, preset, json.RawMessage(widgetRefSchema), nil)); len(terms2) != 0 || again != nil {
		t.Errorf("a second adoption records nothing: %v", terms2)
	}
	// A second rename appends an alias rather than replacing the first.
	preset2 := json.RawMessage(`{"@vocab":"https://schema.org/","cat":"https://example.org/v2#",
	  "maker":"cat:madeBy","supplier":"cat:suppliedBy"}`)
	twice, _, err := adoptTerms(adopted, preset2, conflictMoves(adopted, preset2, json.RawMessage(widgetRefSchema), []string{"cat"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonld.TermAliases(twice)["maker"]; len(got) != 2 {
		t.Errorf("both retired IRIs are kept: %v", got)
	}
}

func TestHeldTermsOf_TellsAddedFromRedefined(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","maker":{"@id":"https://schema.org/maker","@type":"@id"}}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","maker":{"@id":"https://example.org/catalog#madeBy","@type":"@id"},
	  "supplier":{"@id":"https://example.org/catalog#supplier","@type":"@id"}}`)
	rec, err := reconcileAdditiveContext(stored, preset, json.RawMessage(widgetRefSchema))
	if err != nil {
		t.Fatal(err)
	}
	held := heldTermsOf(rec, stored, preset, json.RawMessage(widgetRefSchema))
	kinds := map[string]HeldTermKind{}
	for _, h := range held {
		kinds[h.Term] = h.Kind
	}
	if kinds["maker"] != HeldTermRedefined || kinds["supplier"] != HeldTermAdded {
		t.Errorf("kinds = %v (held %+v)", kinds, held)
	}
}
