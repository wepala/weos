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

package presets_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/pkg/jsonld"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
)

// TestEveryReferencePropertyHasAContextEntry is the packaging guard for issue
// #510. A property the schema marks with `x-resource-type` is stored as a
// JSON-LD edge keyed by its predicate IRI, and the read path maps that IRI back
// to a property name through jsonld.BuildReverseMap — which is built ONLY from
// explicit context terms, because ParseContext skips every `@`-prefixed
// keyword. A reference resolving through `@vocab` therefore has no reverse
// entry, both readers skip its edge — entities.SimplifyJSONLD for the API
// response and extractEdgeColumns for the projection column — so the write is
// persisted and unreadable, while the API still answers 201.
//
// The boot reconcile now reports this condition, but reporting is a backstop.
// A preset shipped with the gap drops writes on every install of a FRESH
// database too, where there is no stored context to reconcile against and
// nothing to merge in. Catching it here is what keeps that from shipping:
// meal-planning's `ingredient` and `meal-plan` types both had it.
func TestEveryReferencePropertyHasAContextEntry(t *testing.T) {
	registry := application.NewPresetRegistry()
	presets.RegisterAll(registry)

	for _, preset := range registry.List() {
		for _, pt := range preset.Types {
			// A type with no schema declares no references, so it has nothing
			// for this guard to check — but it is NOT benign: every data field
			// it is written with is dropped, which the reconcile reports under
			// NoSchema. Assert it rather than skipping past it silently.
			if len(pt.Schema) == 0 {
				t.Errorf("preset %q type %q declares no JSON Schema, so its projection has only "+
					"base columns and every data field written to it is dropped",
					preset.Name, pt.Slug)
				continue
			}
			dropped := referencesWithoutContextEntry(t, pt)
			if len(dropped) > 0 {
				t.Errorf(
					"preset %q type %q declares reference properties %v with no @context entry: "+
						"writes to them are silently dropped. Add a term mapping each to its predicate IRI.",
					preset.Name, pt.Slug, dropped)
			}
		}
	}
}

// referencesWithoutContextEntry mirrors the read path rather than restating it:
// a reference survives only if the reverse map resolves its predicate IRI back
// to its own property name. Checking per property (not just "a term exists")
// also catches two properties mapped to the same IRI, where the reverse map
// collides and only one of them survives.
func referencesWithoutContextEntry(t *testing.T, pt application.PresetResourceType) []string {
	t.Helper()

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(pt.Schema, &schema); err != nil {
		t.Fatalf("type %q has an unparseable schema: %v", pt.Slug, err)
	}
	if len(pt.Context) > 0 {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(pt.Context, &probe); err != nil {
			t.Fatalf("type %q has an unparseable @context: %v", pt.Slug, err)
		}
	}

	reverse := jsonld.BuildReverseMap(pt.Context)
	var dropped []string
	for _, ref := range application.ExtractReferenceProperties(pt.Schema, pt.Context) {
		if reverse[ref.PredicateIRI] != ref.PropertyName {
			dropped = append(dropped, ref.PropertyName)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// TestPresets_ReferencePropertiesReverseMapToTheirOwnName is the epic's
// (#517) registry-wide guard: every reference property of every built-in
// type reverse-maps to its own name. Two properties collapsing onto one
// predicate — what a careless find-and-replace produces — read back as one.
func TestPresets_ReferencePropertiesReverseMapToTheirOwnName(t *testing.T) {
	for _, preset := range presets.NewDefaultRegistry().List() {
		for _, pt := range preset.Types {
			reverse := jsonld.BuildReverseMap(pt.Context)
			vocab, _ := jsonld.ParseContext(pt.Context)
			for _, ref := range application.ExtractReferenceProperties(pt.Schema, pt.Context) {
				got, ok := reverse[ref.PredicateIRI]
				switch {
				case ok && got != ref.PropertyName:
					t.Errorf("%s/%s: %s reverse-maps to %q, not %q", preset.Name, pt.Slug, ref.PredicateIRI, got, ref.PropertyName)
				case !ok && (vocab == "" || ref.PredicateIRI != vocab+ref.PropertyName):
					t.Errorf("%s/%s: %s (property %q) has no reverse entry", preset.Name, pt.Slug, ref.PredicateIRI, ref.PropertyName)
				}
			}
		}
	}
}

// TestPresets_NoPredicateIRIKeepsACompactPrefix: an expanded predicate IRI
// never contains a ':' after its namespace separator. `https://schema.org/
// foaf:knows` is what a compact IRI becomes when its prefix is undeclared and
// @vocab absorbs it — a term that resolves to nowhere anyone defines.
func TestPresets_NoPredicateIRIKeepsACompactPrefix(t *testing.T) {
	for _, preset := range presets.NewDefaultRegistry().List() {
		for _, pt := range preset.Types {
			_, forward := jsonld.ParseContext(pt.Context)
			for term, iri := range forward {
				if jsonld.IsIRIKey(term) {
					continue // a prefix definition maps a prefix to a namespace
				}
				local := iri
				if i := strings.LastIndexAny(iri, "#/"); i >= 0 {
					local = iri[i+1:]
				}
				if strings.Contains(local, ":") {
					t.Errorf("%s/%s: %q resolves to %s, whose local name keeps an undeclared prefix",
						preset.Name, pt.Slug, term, iri)
				}
			}
		}
	}
}

// TestPresets_NoControlKeywordClaimsAPredicateIRI: no control keyword of any
// built-in type expands onto a predicate a property owns — and, after #522,
// none enters the term map at all.
func TestPresets_NoControlKeywordClaimsAPredicateIRI(t *testing.T) {
	for _, preset := range presets.NewDefaultRegistry().List() {
		for _, pt := range preset.Types {
			_, forward := jsonld.ParseContext(pt.Context)
			for key := range jsonld.ControlKeywords {
				if iri, present := forward[key]; present {
					t.Errorf("%s/%s: control keyword %s entered the term map as %s", preset.Name, pt.Slug, key, iri)
				}
			}
			reverse := jsonld.BuildReverseMap(pt.Context)
			for iri, name := range reverse {
				if jsonld.ControlKeywords[name] {
					t.Errorf("%s/%s: %s reverse-maps to the control keyword %s", preset.Name, pt.Slug, iri, name)
				}
			}
		}
	}
}
