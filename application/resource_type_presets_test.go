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

package application_test

import (
	"encoding/json"
	"testing"

	"weos/application"
	"weos/application/presets"
)

func testRegistry() *application.PresetRegistry {
	return presets.NewDefaultRegistry()
}

func TestPresets_AllPresetsExist(t *testing.T) {
	t.Parallel()
	expected := []string{"auth", "core", "ecommerce", "events", "knowledge", "tasks", "website"}
	defs := testRegistry().List()
	if len(defs) != len(expected) {
		t.Fatalf("expected %d presets, got %d", len(expected), len(defs))
	}
	for i, d := range defs {
		if d.Name != expected[i] {
			t.Fatalf("preset[%d] = %q, want %q", i, d.Name, expected[i])
		}
		if len(d.Types) == 0 {
			t.Fatalf("preset %q has no types", d.Name)
		}
	}
}

func TestPresets_NoDuplicateSlugsAcrossPresets(t *testing.T) {
	t.Parallel()
	seen := make(map[string]string)
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if prev, ok := seen[pt.Slug]; ok {
				t.Fatalf("duplicate slug %q in presets %q and %q", pt.Slug, prev, d.Name)
			}
			seen[pt.Slug] = d.Name
		}
	}
}

func TestPresets_AllContextsAreValidJSON(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if len(pt.Context) == 0 {
				continue
			}
			var v any
			if err := json.Unmarshal(pt.Context, &v); err != nil {
				t.Fatalf("preset %q type %q has invalid JSON context: %v", d.Name, pt.Slug, err)
			}
		}
	}
}

func TestPresets_AllSchemasAreValidJSON(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if len(pt.Schema) == 0 {
				continue
			}
			var v any
			if err := json.Unmarshal(pt.Schema, &v); err != nil {
				t.Fatalf("preset %q type %q has invalid JSON schema: %v", d.Name, pt.Slug, err)
			}
		}
	}
}

func TestPresets_GetFound(t *testing.T) {
	t.Parallel()
	d, ok := testRegistry().Get("website")
	if !ok {
		t.Fatal("expected to find 'website' preset")
	}
	if d.Name != "website" {
		t.Fatalf("got name %q, want %q", d.Name, "website")
	}
}

func TestPresets_GetNotFound(t *testing.T) {
	t.Parallel()
	_, ok := testRegistry().Get("nonexistent")
	if ok {
		t.Fatal("expected not to find 'nonexistent' preset")
	}
}

func TestPresets_AllTypesHaveRequiredFields(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if pt.Name == "" {
				t.Fatalf("preset %q has a type with empty name", d.Name)
			}
			if pt.Slug == "" {
				t.Fatalf("preset %q type %q has empty slug", d.Name, pt.Name)
			}
			if pt.Description == "" {
				t.Fatalf("preset %q type %q has empty description", d.Name, pt.Slug)
			}
		}
	}
}

func TestPresets_CoreHasBehaviors(t *testing.T) {
	t.Parallel()
	d, ok := testRegistry().Get("core")
	if !ok {
		t.Fatal("expected to find 'core' preset")
	}
	if d.Behaviors == nil {
		t.Fatal("core preset should have behaviors")
	}
	if _, ok := d.Behaviors["person"]; !ok {
		t.Fatal("core preset should have 'person' behavior")
	}
	if _, ok := d.Behaviors["organization"]; !ok {
		t.Fatal("core preset should have 'organization' behavior")
	}
}

func TestPresets_BehaviorsRegistry(t *testing.T) {
	t.Parallel()
	behaviors := testRegistry().Behaviors()
	if _, ok := behaviors["person"]; !ok {
		t.Fatal("merged behaviors should include 'person'")
	}
	if _, ok := behaviors["organization"]; !ok {
		t.Fatal("merged behaviors should include 'organization'")
	}
}

func TestPresets_AutoInstallFlag(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		switch d.Name {
		case "core", "auth":
			if !d.AutoInstall {
				t.Fatalf("preset %q should be marked as AutoInstall", d.Name)
			}
		default:
			if d.AutoInstall {
				t.Fatalf("preset %q should NOT be marked as AutoInstall", d.Name)
			}
		}
	}
}

func TestPresets_NoReservedSlugCollisions(t *testing.T) {
	t.Parallel()
	reserved := application.ReservedResourceTypeSlugs()
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if reserved[pt.Slug] {
				t.Fatalf("preset %q type slug %q collides with reserved API route", d.Name, pt.Slug)
			}
		}
	}
}
