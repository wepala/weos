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
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// TestEveryReferencePropertyHasAContextEntry is the packaging guard for issue
// #510. A property the schema marks with `x-resource-type` is stored as a
// JSON-LD edge keyed by its predicate IRI, and the read path maps that IRI back
// to a property name through jsonld.BuildReverseMap — which is built ONLY from
// explicit context terms, because ParseContext skips every `@`-prefixed
// keyword. A reference resolving through `@vocab` therefore has no reverse
// entry, FlattenGraph skips its edge, and every write to it is dropped while
// the API still answers 201.
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
			if len(pt.Schema) == 0 {
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
