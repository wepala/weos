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

package mealplanning

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/application"
)

// The golden copy is mini-me's own output for its ten food types, frozen at the
// commit it names. Core cannot import mini-me (a main package that requires
// private presets), so this file is the only byte check that a stored twin
// type sees no change when its definition moves here. Re-take it from mini-me
// before any change to cmd/mini-me/food_preset.go after that commit reaches
// core; TestGoldenCopyNamesItsSource pins which commit it is.
const goldenPath = "testdata/mini_me_food_types.golden.json"

type goldenFoodTypes struct {
	Source struct {
		Repo   string `json:"repo"`
		Commit string `json:"commit"`
		Path   string `json:"path"`
		Blob   string `json:"blob"`
	} `json:"source"`
	// Context and Schema are JSON strings, not objects, so no decoder can
	// re-order a key and hide a byte difference.
	Types []struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		Context     string `json:"context"`
		Schema      string `json:"schema"`
	} `json:"types"`
}

func loadGolden(t *testing.T) goldenFoodTypes {
	t.Helper()
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read the golden copy: %v", err)
	}
	var g goldenFoodTypes
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("decode the golden copy: %v", err)
	}
	return g
}

func mealPlanningPreset(t *testing.T) application.PresetDefinition {
	t.Helper()
	registry := application.NewPresetRegistry()
	Register(registry)
	def, ok := registry.Get("meal-planning")
	if !ok {
		t.Fatal("Register did not add the meal-planning preset")
	}
	return def
}

// byteMismatch compares two serialized values as bytes. Equal JSON with keys in
// another order is a mismatch: the stored @context keeps its string form, and
// a reordered one is a different stored definition.
func byteMismatch(slug, field, want, got string) string {
	if want == got {
		return ""
	}
	return slug + ": " + field + " differs from the golden copy\n  golden: " + want + "\n  core:   " + got
}

func TestGoldenCopyNamesItsSource(t *testing.T) {
	src := loadGolden(t).Source
	want := map[string][2]string{
		"repo":   {src.Repo, "wepala/mini-me-weos"},
		"commit": {src.Commit, "fbf90ba6"},
		"path":   {src.Path, "cmd/mini-me/food_preset.go"},
		"blob":   {src.Blob, "99f92d2b7170087af64223387460f79e783a7e30"},
	}
	for field, pair := range want {
		if pair[0] == "" {
			t.Errorf("the golden copy records no source %s", field)
			continue
		}
		if pair[0] != pair[1] {
			t.Errorf("the golden copy records source %s %q, want %q", field, pair[0], pair[1])
		}
	}
}

func TestMovedFoodTypesMatchMiniMeGolden(t *testing.T) {
	golden := loadGolden(t)
	if len(golden.Types) != 10 {
		t.Fatalf("the golden copy holds %d types, want 10", len(golden.Types))
	}
	registered := map[string]application.PresetResourceType{}
	for _, pt := range mealPlanningPreset(t).Types {
		registered[pt.Slug] = pt
	}
	for _, g := range golden.Types {
		pt, ok := registered[g.Slug]
		if !ok {
			t.Errorf("meal-planning does not register the golden type %q", g.Slug)
			continue
		}
		for _, m := range []string{
			byteMismatch(g.Slug, "name", g.Name, pt.Name),
			byteMismatch(g.Slug, "slug", g.Slug, pt.Slug),
			byteMismatch(g.Slug, "description", g.Description, pt.Description),
			byteMismatch(g.Slug, "context", g.Context, string(pt.Context)),
			byteMismatch(g.Slug, "schema", g.Schema, string(pt.Schema)),
		} {
			if m != "" {
				t.Error(m)
			}
		}
	}
}

func TestGoldenComparisonIsByteExact(t *testing.T) {
	a := `{"@vocab":"https://schema.org/","@type":"Restaurant"}`
	b := `{"@type":"Restaurant","@vocab":"https://schema.org/"}`
	var ja, jb map[string]any
	if json.Unmarshal([]byte(a), &ja) != nil || json.Unmarshal([]byte(b), &jb) != nil {
		t.Fatal("the fixtures must be valid JSON")
	}
	if ja["@vocab"] != jb["@vocab"] || ja["@type"] != jb["@type"] || len(ja) != len(jb) {
		t.Fatal("the fixtures must be equal as JSON")
	}
	if byteMismatch("restaurant", "context", a, b) == "" {
		t.Error("byteMismatch accepted two contexts that differ in key order; the check must compare bytes")
	}
	if got := byteMismatch("restaurant", "context", a, a); got != "" {
		t.Errorf("byteMismatch reported identical bytes as different: %s", got)
	}
}

func TestRegister_ListsTwentyFourTypes(t *testing.T) {
	def := mealPlanningPreset(t)
	want := []string{
		"recipe", "how-to-step", "ingredient", "recipe-ingredient", "nutrition-information",
		"cookbook", "meal-plan", "scheduled-meal", "meal-occurrence", "pantry", "food-item",
		"shopping-list", "shopping-list-item", "restricted-diet",
		"taste-profile", "meal-log", "restaurant", "planned-meal", "grocery-amendment",
		"grocery-list-item", "staple", "purchase", "purchase-line", "item-kind",
	}
	got := make([]string, 0, len(def.Types))
	for _, pt := range def.Types {
		got = append(got, pt.Slug)
	}
	sort.Strings(want)
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("meal-planning registers %d types, want %d\n  got:  %v\n  want: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("meal-planning registers %v, want %v", got, want)
		}
	}
	if def.AutoInstall {
		t.Error("meal-planning must not auto-install in core; a product turns it on")
	}
}

func TestSidebarHidesDerivedFoodTypes(t *testing.T) {
	def := mealPlanningPreset(t)
	if def.Sidebar == nil {
		t.Fatal("meal-planning declares no sidebar config")
	}
	hidden := map[string]bool{}
	for _, slug := range def.Sidebar.HiddenSlugs {
		hidden[slug] = true
	}
	for _, slug := range []string{
		"how-to-step", "recipe-ingredient", "nutrition-information",
		"meal-occurrence", "food-item", "shopping-list-item", "restricted-diet",
		"grocery-list-item", "grocery-amendment", "purchase-line",
	} {
		if !hidden[slug] {
			t.Errorf("the sidebar does not hide %q", slug)
		}
	}
	// mini-me's live twin shows every food type in its admin today; only the
	// three derived ones above may be hidden.
	for _, slug := range []string{
		"taste-profile", "meal-log", "restaurant", "planned-meal", "staple", "purchase", "item-kind",
	} {
		if hidden[slug] {
			t.Errorf("the sidebar hides %q, a type mini-me's live twin uses", slug)
		}
	}
}

func TestSidebarGroupsEachTwinPairUnderOneVisibleParent(t *testing.T) {
	def := mealPlanningPreset(t)
	if def.Sidebar == nil {
		t.Fatal("meal-planning declares no sidebar config")
	}
	hidden := map[string]bool{}
	for _, slug := range def.Sidebar.HiddenSlugs {
		hidden[slug] = true
	}
	groups := def.Sidebar.MenuGroups
	for _, pair := range [][2]string{
		{"planned-meal", "scheduled-meal"},
		{"meal-log", "meal-occurrence"},
		{"grocery-list-item", "shopping-list-item"},
	} {
		twin, core := groups[pair[0]], groups[pair[1]]
		if twin == "" || twin != core {
			t.Errorf("%s is grouped under %q and %s under %q; a twin pair must share one parent",
				pair[0], twin, pair[1], core)
			continue
		}
		// The admin nests an entry under its parent only when the parent is shown.
		if hidden[twin] {
			t.Errorf("%s and %s are grouped under %q, which the sidebar hides", pair[0], pair[1], twin)
		}
	}
	if got := groups["purchase"]; got != "shopping-list" {
		t.Errorf("purchase is grouped under %q, want the shopping-list area", got)
	}
	for _, slug := range []string{
		"scheduled-meal", "meal-occurrence", "shopping-list-item",
		"planned-meal", "meal-log", "grocery-list-item",
	} {
		if !strings.Contains(def.Description, slug) {
			t.Errorf("the preset description does not say which of the twin pair %q belongs to", slug)
		}
	}
}
