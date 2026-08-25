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

// TestPresets_ContextGuards sweeps the whole built-in registry with the
// epic's (#517) vocabulary guards — every reference property reverse-maps to
// its own name, no expanded predicate keeps an undeclared compact prefix, no
// control keyword claims a predicate (#522). presets.ContextGuardViolations
// carries the rules so a private registry can sweep itself.
func TestPresets_ContextGuards(t *testing.T) {
	for _, preset := range presets.NewDefaultRegistry().List() {
		for _, line := range presets.ContextGuardViolations(preset.Types) {
			t.Errorf("%s/%s", preset.Name, line)
		}
	}
}

// TestPresets_ContextGuardsCatchTheKnownBadShapes proves the sweep has teeth:
// each rule fires on the fixture it exists for.
func TestPresets_ContextGuardsCatchTheKnownBadShapes(t *testing.T) {
	bad := []application.PresetResourceType{{
		Slug: "widget",
		Context: json.RawMessage(`{"@vocab":"https://schema.org/","maker":"https://schema.org/associated",` +
			`"partner":"https://schema.org/associated","knows":"foaf:knows","foaf:knows":"foaf:knows"}`),
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"maker":{"type":"string","x-resource-type":"vendor"},"partner":{"type":"string","x-resource-type":"vendor"}}}`),
	}}
	lines := strings.Join(presets.ContextGuardViolations(bad), "\n")
	for _, want := range []string{"reverse-maps to", `"knows" resolves to https://schema.org/foaf:knows`,
		`"foaf:knows" resolves to https://schema.org/foaf:knows`} {
		if !strings.Contains(lines, want) {
			t.Errorf("the guards did not report %q:\n%s", want, lines)
		}
	}
	// A control keyword is skipped by ParseContext now, and a URN-valued term
	// in a context with no @vocab has no local name to test: both clean.
	control := []application.PresetResourceType{
		{Slug: "old", Context: json.RawMessage(
			`{"@vocab":"https://schema.org/","rdfs:subClassOf":"maker","maker":"https://schema.org/maker"}`)},
		{Slug: "urn", Context: json.RawMessage(`{"kind":"urn:type:widget"}`)},
	}
	if v := presets.ContextGuardViolations(control); len(v) != 0 {
		t.Errorf("a control keyword is skipped by ParseContext now; the guard must see a clean type: %v", v)
	}
}
