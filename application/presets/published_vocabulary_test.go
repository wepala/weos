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

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
)

// Issue #535. These sweep the registry rather than the three types the issue
// names: nothing stops a fourth type copying the shape, and the boot treats
// them all alike. They live here rather than in the e2e suite, following the
// precedent #522 set, because they are a pure function of the registry — no
// database, no boot — so they run in `make test-unit` on every change instead
// of only in the e2e job.

// allBuiltInTypes flattens the registry. Type slugs are unique across it
// (asserted below), which is what lets a waiver key on slug alone.
func allBuiltInTypes(t *testing.T) []application.PresetResourceType {
	t.Helper()
	var types []application.PresetResourceType
	for _, preset := range presets.NewDefaultRegistry().List() {
		types = append(types, preset.Types...)
	}
	if len(types) == 0 {
		t.Fatal("the registry produced no types; the sweep would pass by looking at nothing")
	}
	return types
}

// TestPresets_TypeSlugsAreUniqueAcrossTheRegistry underwrites the waiver key.
// Were two presets to share a slug, one waiver line would silently cover both.
func TestPresets_TypeSlugsAreUniqueAcrossTheRegistry(t *testing.T) {
	owners := map[string][]string{}
	for _, preset := range presets.NewDefaultRegistry().List() {
		for _, pt := range preset.Types {
			owners[pt.Slug] = append(owners[pt.Slug], preset.Name)
		}
	}
	for slug, presetNames := range owners {
		if len(presetNames) > 1 {
			t.Errorf("slug %q is declared by %v; a waiver keyed on it would cover both", slug, presetNames)
		}
	}
}

// TestPresets_NoPropertyClaimsAnUndefinedPublishedTerm is the issue's criterion.
//
// It asserts the violation set EQUALS the waivers rather than merely excluding
// meal-planning. Exact equality is what makes the list only ever shrink: a new
// offender anywhere fails on the day it is authored, and repairing one fails
// until its waiver line is deleted.
func TestPresets_NoPropertyClaimsAnUndefinedPublishedTerm(t *testing.T) {
	assertViolationsEqualWaivers(t, presets.FaultUndefinedTerm)
}

// TestPresets_NoPropertyClaimsAPublishedTermForAnotherSubject is the half an
// allow-list cannot reach: the term resolves, so nothing about looking it up
// reveals the problem. Without this, `schema:status` walks back in on the next
// type somebody writes.
func TestPresets_NoPropertyClaimsAPublishedTermForAnotherSubject(t *testing.T) {
	assertViolationsEqualWaivers(t, presets.FaultOtherSubject)
}

func assertViolationsEqualWaivers(t *testing.T, fault presets.VocabularyFault) {
	t.Helper()
	waivers := presets.VocabularyWaivers()
	mealPlanning := mealPlanningSlugs()
	for _, v := range presets.PublishedVocabularyViolations(allBuiltInTypes(t)) {
		if v.Fault != fault {
			continue
		}
		if _, waived := waivers[v.Key()]; !waived {
			// Name the property and the IRI on its own line. A bare count
			// tells whoever broke it nothing about what to fix.
			t.Errorf("unwaived violation: %s", v)
		}
		if mealPlanning[v.Slug] {
			t.Errorf("#535 repaired meal-planning; %s must not be waived or violating", v)
		}
	}
}

// TestPresets_NoWaiverOutlivesItsViolation is the other half of the equality,
// and it is a separate test because folding it into the two fault-specific
// sweeps made it DEAD CODE: a waiver naming nothing at all exhibits no fault,
// so it matched neither sweep's filter and was silently tolerated. The list
// could then only grow. Proved by adding a waiver for a property that does not
// exist and watching the suite stay green.
//
// A waiver that outlives its violation is not harmless. Every entry is a
// standing permission for one property to state a predicate its vocabulary
// does not define, and a stale one silently re-permits that name the moment
// somebody reuses it.
func TestPresets_NoWaiverOutlivesItsViolation(t *testing.T) {
	violations := map[string]bool{}
	for _, v := range presets.PublishedVocabularyViolations(allBuiltInTypes(t)) {
		violations[v.Key()] = true
	}
	for key := range presets.VocabularyWaivers() {
		if !violations[key] {
			t.Errorf("stale waiver %q: nothing violates any more, so delete the line", key)
		}
	}
}

// mealPlanningSlugs is computed once per call rather than per violation; the
// registry is rebuilt from scratch on every List() and the sweep asks this
// question for every entry it reports.
func mealPlanningSlugs() map[string]bool {
	out := map[string]bool{}
	for _, preset := range presets.NewDefaultRegistry().List() {
		if preset.Name != "meal-planning" {
			continue
		}
		for _, pt := range preset.Types {
			out[pt.Slug] = true
		}
	}
	return out
}

// TestPresets_EveryAllowListedTermIsStillUsed keeps the curated list honest.
// An allow-list nobody prunes becomes a rubber stamp: every stale entry is a
// name a future property could adopt without anyone re-checking that it fits.
func TestPresets_EveryAllowListedTermIsStillUsed(t *testing.T) {
	for _, stale := range presets.UnusedAllowListEntries(allBuiltInTypes(t)) {
		t.Errorf("allow-listed term %s is no longer resolved by any type; prune it", stale)
	}
}

// TestPresets_TheGuardNamesAnUntermedHouseProperty proves the guard BITES, and
// bites on the shape that caused #535 — a house property added with no term,
// riding `@vocab` into somebody else's namespace. A sweep that passes by never
// looking at anything is indistinguishable from a green one otherwise.
func TestPresets_TheGuardNamesAnUntermedHouseProperty(t *testing.T) {
	victim := application.PresetResourceType{
		Slug:    "food-item",
		Context: json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Thing"}`),
		Schema:  json.RawMessage(`{"type":"object","properties":{"spiciness":{"type":"string"}}}`),
	}
	got := presets.PublishedVocabularyViolations([]application.PresetResourceType{victim})
	if len(got) != 1 {
		t.Fatalf("expected exactly one violation, got %d: %v", len(got), got)
	}
	if got[0].PropertyName != "spiciness" ||
		got[0].PredicateIRI != "https://schema.org/spiciness" ||
		got[0].Fault != presets.FaultUndefinedTerm {
		t.Errorf("the guard named the wrong thing: %s", got[0])
	}
}

// TestPresets_TheGuardNamesATermPublishedForAnotherSubject is the deny-list
// half biting. Without it the guard degrades to a 404 checker.
func TestPresets_TheGuardNamesATermPublishedForAnotherSubject(t *testing.T) {
	victim := application.PresetResourceType{
		Slug:    "pantry",
		Context: json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Thing"}`),
		Schema:  json.RawMessage(`{"type":"object","properties":{"status":{"type":"string"}}}`),
	}
	got := presets.PublishedVocabularyViolations([]application.PresetResourceType{victim})
	if len(got) != 1 || got[0].Fault != presets.FaultOtherSubject {
		t.Fatalf("expected one other-subject violation, got %v", got)
	}
	if !strings.Contains(got[0].Detail, "Medical") {
		t.Errorf("the violation should say what the term really means, got %q", got[0].Detail)
	}
}

// TestPresets_AHousePredicateIsNotPoliced — WeOS publishes its own vocabulary,
// so WeOS defines whatever it names there. #520's guard polices that
// namespace's shape; this one must stay out of its way.
func TestPresets_AHousePredicateIsNotPoliced(t *testing.T) {
	victim := application.PresetResourceType{
		Slug: "food-item",
		Context: json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Thing",
			"mp":"https://weos.io/vocab/meal-planning#","whatever":"mp:whatever"}`),
		Schema: json.RawMessage(`{"type":"object","properties":{"whatever":{"type":"string"}}}`),
	}
	if got := presets.PublishedVocabularyViolations([]application.PresetResourceType{victim}); len(got) != 0 {
		t.Errorf("house predicates must not be policed, got %v", got)
	}
}

// TestPresets_MealPlanningStatesOnlyTermsItsVocabulariesDefine is #535's own
// acceptance criterion, asserted directly rather than inferred from the
// registry-wide equality above.
func TestPresets_MealPlanningStatesOnlyTermsItsVocabulariesDefine(t *testing.T) {
	var mealPlanning []application.PresetResourceType
	for _, preset := range presets.NewDefaultRegistry().List() {
		if preset.Name == "meal-planning" {
			mealPlanning = preset.Types
		}
	}
	if len(mealPlanning) == 0 {
		t.Fatal("the meal-planning preset is missing; the sweep would pass by looking at nothing")
	}
	for _, v := range presets.PublishedVocabularyViolations(mealPlanning) {
		t.Errorf("meal-planning still states a term its vocabulary does not define: %s", v)
	}
}

// TestPresets_TheRepairedMealPlanningPredicates pins the specific IRIs #535
// settled, including the two that deliberately did NOT move to the house
// namespace. The second group is the one that fails if the fix over-corrects:
// dragging a genuine published name into the house vocabulary breaks no read,
// so nothing else in the suite would notice.
func TestPresets_TheRepairedMealPlanningPredicates(t *testing.T) {
	const mp = "https://weos.io/vocab/meal-planning#"
	const schema = "https://schema.org/"
	want := map[string]map[string]string{
		"recipe-ingredient": {
			"quantity": mp + "quantity", "unit": mp + "unit",
			"optional": mp + "optional", "preparation": mp + "preparation",
			"ingredient": mp + "ingredient",
		},
		"meal-occurrence": {
			"cookedAt": mp + "cookedAt", "status": mp + "status",
			// The published spelling, not a minted `mp:date`: schema.org
			// defines startDate on Schedule and Event, and a meal occurrence
			// is an event happening on a date.
			"date": schema + "startDate",
		},
		"pantry":        {"isDefault": mp + "isDefault"},
		"shopping-list": {"createdAt": mp + "createdAt", "status": mp + "status"},
		"food-item": {
			"quantity": mp + "quantity", "unit": mp + "unit",
			"storage": mp + "storage", "expirationDate": mp + "expirationDate",
			// Sits on the same type as four repairs and must not move with
			// them: schema.org publishes purchaseDate in exactly this sense.
			"purchaseDate": schema + "purchaseDate",
		},
		"shopping-list-item": {"quantity": mp + "quantity", "unit": mp + "unit", "checked": mp + "checked"},
		"ingredient":         {"shoppingCategory": mp + "shoppingCategory", "season": mp + "season"},
		// The food ontology defines no `hasIngredient`; schema.org defines
		// recipeIngredient for precisely this.
		"recipe":    {"recipeIngredient": schema + "recipeIngredient"},
		"meal-plan": {"startDate": schema + "startDate", "endDate": schema + "endDate"},
		"scheduled-meal": {
			"startTime": schema + "startTime", "repeatFrequency": schema + "repeatFrequency",
			"scheduleTimezone": schema + "scheduleTimezone",
		},
		// The rest of the names a careless sweep would take on its way past:
		// a whole NutritionInformation, a whole HowToStep, and the two that
		// appear on nearly every type.
		"nutrition-information": {"servingSize": schema + "servingSize"},
		"how-to-step":           {"position": schema + "position"},
		"restricted-diet":       {"identifier": schema + "identifier"},
	}
	want["recipe"]["recipeYield"] = schema + "recipeYield"
	want["pantry"]["name"] = schema + "name"
	want["pantry"]["description"] = schema + "description"
	bySlug := map[string]application.PresetResourceType{}
	for _, preset := range presets.NewDefaultRegistry().List() {
		for _, pt := range preset.Types {
			bySlug[pt.Slug] = pt
		}
	}
	slugs := make([]string, 0, len(want))
	for slug := range want {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		pt, ok := bySlug[slug]
		if !ok {
			t.Errorf("type %q is missing from the registry", slug)
			continue
		}
		for prop, wantIRI := range want[slug] {
			if got := presets.ResolvedPredicateFor(pt, prop); got != wantIRI {
				t.Errorf("%s.%s resolves to %s, want %s", slug, prop, got, wantIRI)
			}
		}
	}
}
