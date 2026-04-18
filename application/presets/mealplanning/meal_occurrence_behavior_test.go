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
	"context"
	"encoding/json"
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
)

// setupDepletionBehavior builds a stub-backed depletion behavior with
// a scheduled meal, recipe, recipe ingredients, and food items already
// seeded. Returns the stub so tests can inspect/mutate it.
func setupDepletionBehavior(t *testing.T) (
	*depletePantryOnCookBehavior, *stubResourceSvc,
) {
	t.Helper()
	stub := newStubResourceSvc()
	b := newDepletePantryOnCookBehavior(testBehaviorServices(stub))

	// Seed Recipe with yield=2.
	recipe := makeTestResource(t, "recipe-1", "recipe", map[string]any{
		"name": "Pasta",
		"recipeYield": map[string]any{
			"value":    float64(2),
			"unitText": "servings",
		},
	})
	stub.getByIDData["recipe-1"] = recipe

	// Seed flat projection rows. loadFlatRow queries via
	// FindAllByTypeFlatWithFilters with id eq <id>, so we seed full rows
	// including FK values for reference properties.
	stub.listFlatData["meal-occurrence"] = []map[string]any{
		{
			"id": "occ-1", "date": "2026-04-15", "mealType": "dinner",
			"status": "cooked", "servings": float64(2), "scheduledMeal": "sm-1",
		},
	}
	stub.listFlatData["scheduled-meal"] = []map[string]any{
		{"id": "sm-1", "recipe": "recipe-1"},
	}
	// Projection rows store object-valued fields as JSON strings.
	stub.listFlatData["recipe"] = []map[string]any{
		{
			"id": "recipe-1", "name": "Pasta",
			"recipeYield": `{"value":2,"unitText":"servings"}`,
		},
	}
	// Seed a default pantry (depletion always uses the default pantry).
	stub.listFlatData["pantry"] = []map[string]any{
		{"id": "pantry-1", "isDefault": true, "name": "Home"},
	}

	// Seed RecipeIngredient list.
	stub.listFlatData["recipe-ingredient"] = []map[string]any{
		{
			"id":         "ri-1",
			"recipe":     "recipe-1",
			"ingredient": "ing-1",
			"quantity":   float64(100),
			"unit":       "g",
		},
	}

	return b, stub
}

// makeCookedOccurrence returns a resource representing a cooked MealOccurrence.
// servings defaults to 2 (matching recipe yield → scale 1.0).
func makeCookedOccurrence(t *testing.T, servings float64) *entities.Resource {
	t.Helper()
	return makeTestResource(t, "occ-1", "meal-occurrence", map[string]any{
		"date":          "2026-04-15",
		"mealType":      "dinner",
		"status":        "cooked",
		"servings":      servings,
		"scheduledMeal": "sm-1",
	})
}

func TestDeplete_StatusGuard_NoDepletionWhenNotCooked(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)
	stub.listFlatData["food-item"] = []map[string]any{
		{"id": "fi-1", "pantry": "pantry-1", "ingredient": "ing-1",
			"quantity": float64(500), "unit": "g"},
	}
	// Override the occurrence flat row to be planned (not cooked).
	stub.listFlatData["meal-occurrence"] = []map[string]any{
		{"id": "occ-1", "date": "2026-04-15", "mealType": "dinner",
			"status": "planned", "servings": float64(2), "scheduledMeal": "sm-1"},
	}

	resource := makeTestResource(t, "occ-1", "meal-occurrence", map[string]any{
		"status": "planned",
	})

	// BeforeUpdate sees planned→planned (no transition); AfterUpdate must no-op.
	_, _ = b.BeforeUpdate(context.Background(), resource, resource.Data(), nil)
	if err := b.AfterUpdate(context.Background(), resource); err != nil {
		t.Fatalf("AfterUpdate returned error: %v", err)
	}
	if len(stub.updates) != 0 {
		t.Fatalf("expected no updates for non-cooked status, got %d", len(stub.updates))
	}
}

func TestDeplete_ReDepletionGuard(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)
	stub.listFlatData["food-item"] = []map[string]any{
		{"id": "fi-1", "pantry": "pantry-1", "ingredient": "ing-1",
			"quantity": float64(500), "unit": "g"},
	}

	// Existing resource is ALREADY cooked. Another update that does not
	// change status should NOT trigger depletion.
	existing := makeCookedOccurrence(t, 2)

	// Simulate an update that changes notes but leaves status=cooked.
	newData := map[string]any{
		"date":          "2026-04-15",
		"mealType":      "dinner",
		"status":        "cooked",
		"servings":      float64(2),
		"scheduledMeal": "sm-1",
		"notes":         "edited",
	}
	dataBytes, _ := json.Marshal(newData)
	_, _ = b.BeforeUpdate(context.Background(), existing, dataBytes, nil)
	if err := b.AfterUpdate(context.Background(), existing); err != nil {
		t.Fatalf("AfterUpdate returned error: %v", err)
	}
	if len(stub.updates) != 0 {
		t.Fatalf("expected no pantry updates on cooked→cooked edit, got %d",
			len(stub.updates))
	}
}

func TestDeplete_TransitionToCookedDepletesOnce(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)
	stub.listFlatData["food-item"] = []map[string]any{
		{"id": "fi-1", "pantry": "pantry-1", "ingredient": "ing-1",
			"quantity": float64(500), "unit": "g"},
	}

	// Existing is planned, new is cooked → transition, should deplete.
	existing := makeTestResource(t, "occ-1", "meal-occurrence", map[string]any{
		"status": "planned",
	})
	newData := map[string]any{
		"date": "2026-04-15", "mealType": "dinner", "status": "cooked",
		"servings": float64(2), "scheduledMeal": "sm-1",
	}
	dataBytes, _ := json.Marshal(newData)
	_, _ = b.BeforeUpdate(context.Background(), existing, dataBytes, nil)

	// AfterUpdate receives the updated resource (cooked).
	updated := makeCookedOccurrence(t, 2)
	if err := b.AfterUpdate(context.Background(), updated); err != nil {
		t.Fatalf("AfterUpdate returned error: %v", err)
	}
	if len(stub.updates) != 1 {
		t.Fatalf("expected 1 food item update, got %d", len(stub.updates))
	}
	var upd map[string]any
	_ = json.Unmarshal(stub.updates[0].Data, &upd)
	if toFloat(upd["quantity"]) != 400 {
		t.Fatalf("expected quantity 400 (500-100), got %v", upd["quantity"])
	}
}

func TestDeplete_ScalingFactor(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)
	stub.listFlatData["food-item"] = []map[string]any{
		{"id": "fi-1", "pantry": "pantry-1", "ingredient": "ing-1",
			"quantity": float64(1000), "unit": "g"},
	}
	// Override flat row to 4 servings for scaling test.
	stub.listFlatData["meal-occurrence"] = []map[string]any{
		{"id": "occ-1", "date": "2026-04-15", "mealType": "dinner",
			"status": "cooked", "servings": float64(4), "scheduledMeal": "sm-1"},
	}
	// 4 servings / 2 yield = 2.0 scale → 200g needed from 1000g.
	existing := makeTestResource(t, "occ-1", "meal-occurrence",
		map[string]any{"status": "planned"})
	newData := map[string]any{"status": "cooked"}
	dataBytes, _ := json.Marshal(newData)
	_, _ = b.BeforeUpdate(context.Background(), existing, dataBytes, nil)

	updated := makeCookedOccurrence(t, 4)
	_ = b.AfterUpdate(context.Background(), updated)

	if len(stub.updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(stub.updates))
	}
	var upd map[string]any
	_ = json.Unmarshal(stub.updates[0].Data, &upd)
	if toFloat(upd["quantity"]) != 800 {
		t.Fatalf("expected 800 (1000-200), got %v", upd["quantity"])
	}
}

func TestDeplete_FIFOByExpiration(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)
	// Three items: expiring 2026-05-01, 2026-04-01, no date.
	// Expect the 2026-04-01 drained first.
	stub.listFlatData["food-item"] = []map[string]any{
		{"id": "fi-late", "pantry": "pantry-1", "ingredient": "ing-1",
			"quantity": float64(50), "unit": "g",
			"expirationDate": "2026-05-01"},
		{"id": "fi-early", "pantry": "pantry-1", "ingredient": "ing-1",
			"quantity": float64(60), "unit": "g",
			"expirationDate": "2026-04-01"},
		{"id": "fi-nodate", "pantry": "pantry-1", "ingredient": "ing-1",
			"quantity": float64(200), "unit": "g"},
	}

	existing := makeTestResource(t, "occ-1", "meal-occurrence",
		map[string]any{"status": "planned"})
	dataBytes, _ := json.Marshal(map[string]any{"status": "cooked"})
	_, _ = b.BeforeUpdate(context.Background(), existing, dataBytes, nil)
	_ = b.AfterUpdate(context.Background(), makeCookedOccurrence(t, 2))

	// Need 100g. fi-early has 60g (drained to 0), remaining 40 from fi-late.
	// fi-nodate should be untouched.
	if len(stub.updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(stub.updates))
	}
	updatedIDs := map[string]float64{}
	for _, u := range stub.updates {
		var m map[string]any
		_ = json.Unmarshal(u.Data, &m)
		updatedIDs[u.ID] = toFloat(m["quantity"])
	}
	if updatedIDs["fi-early"] != 0 {
		t.Fatalf("expected fi-early drained to 0, got %v", updatedIDs["fi-early"])
	}
	if updatedIDs["fi-late"] != 10 {
		t.Fatalf("expected fi-late reduced to 10 (50-40), got %v", updatedIDs["fi-late"])
	}
	if _, touched := updatedIDs["fi-nodate"]; touched {
		t.Fatal("fi-nodate should not have been touched")
	}
}

func TestDeplete_UnitMismatchSkipsAndWarns(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)
	stub.listFlatData["food-item"] = []map[string]any{
		{"id": "fi-1", "pantry": "pantry-1", "ingredient": "ing-1",
			"quantity": float64(10), "unit": "oz"}, // mismatch: recipe wants "g"
	}

	ctx := entities.ContextWithMessages(context.Background())
	existing := makeTestResource(t, "occ-1", "meal-occurrence",
		map[string]any{"status": "planned"})
	dataBytes, _ := json.Marshal(map[string]any{"status": "cooked"})
	_, _ = b.BeforeUpdate(ctx, existing, dataBytes, nil)
	_ = b.AfterUpdate(ctx, makeCookedOccurrence(t, 2))

	if len(stub.updates) != 0 {
		t.Fatalf("expected no updates on unit mismatch, got %d", len(stub.updates))
	}
	msgs := entities.GetMessages(ctx)
	foundMismatch := false
	for _, m := range msgs {
		if m.Code == "pantry_depletion_unit_mismatch" {
			foundMismatch = true
		}
	}
	if !foundMismatch {
		t.Fatalf("expected unit_mismatch warning, got %v", msgs)
	}
}

func TestDeplete_ShortfallOnEmptyPantry(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)
	stub.listFlatData["food-item"] = nil // no items at all

	ctx := entities.ContextWithMessages(context.Background())
	existing := makeTestResource(t, "occ-1", "meal-occurrence",
		map[string]any{"status": "planned"})
	dataBytes, _ := json.Marshal(map[string]any{"status": "cooked"})
	_, _ = b.BeforeUpdate(ctx, existing, dataBytes, nil)
	_ = b.AfterUpdate(ctx, makeCookedOccurrence(t, 2))

	found := false
	for _, m := range entities.GetMessages(ctx) {
		if m.Code == "pantry_depletion_shortfall" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected shortfall warning for empty pantry")
	}
}

func TestDeplete_ShortfallWhenPartiallyCovered(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)
	stub.listFlatData["food-item"] = []map[string]any{
		{"id": "fi-1", "pantry": "pantry-1", "ingredient": "ing-1",
			"quantity": float64(30), "unit": "g"},
	}

	ctx := entities.ContextWithMessages(context.Background())
	existing := makeTestResource(t, "occ-1", "meal-occurrence",
		map[string]any{"status": "planned"})
	dataBytes, _ := json.Marshal(map[string]any{"status": "cooked"})
	_, _ = b.BeforeUpdate(ctx, existing, dataBytes, nil)
	_ = b.AfterUpdate(ctx, makeCookedOccurrence(t, 2))

	// 30g available, 100g needed → shortfall 70g, fi-1 drained to 0.
	if len(stub.updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(stub.updates))
	}
	var upd map[string]any
	_ = json.Unmarshal(stub.updates[0].Data, &upd)
	if toFloat(upd["quantity"]) != 0 {
		t.Fatalf("expected fi-1 drained to 0, got %v", upd["quantity"])
	}
	found := false
	for _, m := range entities.GetMessages(ctx) {
		if m.Code == "pantry_depletion_shortfall" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected shortfall warning")
	}
}

func TestDeplete_PantryResolution_DefaultPantry(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)
	// Pre-seed a food item in the expected pantry.
	stub.listFlatData["food-item"] = []map[string]any{
		{"id": "fi-1", "pantry": "pantry-1", "ingredient": "ing-1",
			"quantity": float64(500), "unit": "g"},
	}

	existing := makeTestResource(t, "occ-1", "meal-occurrence",
		map[string]any{"status": "planned"})
	dataBytes, _ := json.Marshal(map[string]any{"status": "cooked"})
	_, _ = b.BeforeUpdate(context.Background(), existing, dataBytes, nil)
	_ = b.AfterUpdate(context.Background(), makeCookedOccurrence(t, 2))

	// Update should target the food item in pantry-1 (default pantry).
	if len(stub.updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(stub.updates))
	}
}

func TestDeplete_PantryResolution_FallbackToDefault(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)

	// Override scheduled meal flat row to have no mealPlan.
	stub.listFlatData["scheduled-meal"] = []map[string]any{
		{"id": "sm-1", "recipe": "recipe-1"},
	}

	// Pre-seed a default pantry and a food item in it.
	stub.listFlatData["pantry"] = []map[string]any{
		{"id": "default-pantry", "isDefault": true, "name": "Home"},
	}
	stub.listFlatData["food-item"] = []map[string]any{
		{"id": "fi-1", "pantry": "default-pantry", "ingredient": "ing-1",
			"quantity": float64(500), "unit": "g"},
	}

	existing := makeTestResource(t, "occ-1", "meal-occurrence",
		map[string]any{"status": "planned"})
	dataBytes, _ := json.Marshal(map[string]any{"status": "cooked"})
	_, _ = b.BeforeUpdate(context.Background(), existing, dataBytes, nil)
	_ = b.AfterUpdate(context.Background(), makeCookedOccurrence(t, 2))

	if len(stub.updates) != 1 {
		t.Fatalf("expected 1 update targeting default pantry, got %d", len(stub.updates))
	}
}

func TestDeplete_NoPantryResolvedEmitsWarning(t *testing.T) {
	t.Parallel()
	b, stub := setupDepletionBehavior(t)
	// Override scheduled meal to have no mealPlan AND no default pantry.
	stub.listFlatData["scheduled-meal"] = []map[string]any{
		{"id": "sm-1", "recipe": "recipe-1"},
	}
	stub.listFlatData["pantry"] = nil

	ctx := entities.ContextWithMessages(context.Background())
	existing := makeTestResource(t, "occ-1", "meal-occurrence",
		map[string]any{"status": "planned"})
	dataBytes, _ := json.Marshal(map[string]any{"status": "cooked"})
	_, _ = b.BeforeUpdate(ctx, existing, dataBytes, nil)
	_ = b.AfterUpdate(ctx, makeCookedOccurrence(t, 2))

	found := false
	for _, m := range entities.GetMessages(ctx) {
		if m.Code == "pantry_depletion_no_pantry" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected pantry_depletion_no_pantry warning")
	}
}

func TestDeplete_NilServiceEmitsWarning(t *testing.T) {
	t.Parallel()
	b := newDepletePantryOnCookBehavior(application.BehaviorServices{})
	// No writer injected.
	ctx := entities.ContextWithMessages(context.Background())
	// Mark pending manually so deplete runs.
	b.markPending("occ-1")
	if err := b.AfterUpdate(ctx, makeCookedOccurrence(t, 2)); err != nil {
		t.Fatalf("AfterUpdate returned error: %v", err)
	}
	found := false
	for _, m := range entities.GetMessages(ctx) {
		if m.Code == "behavior_dependency_missing" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected behavior_dependency_missing warning")
	}
}

func TestToFloat_StringParse(t *testing.T) {
	t.Parallel()
	if toFloat("2.5") != 2.5 {
		t.Fatalf("expected 2.5, got %v", toFloat("2.5"))
	}
	if toFloat("not-a-number") != 0 {
		t.Fatalf("expected 0 for bad string, got %v", toFloat("not-a-number"))
	}
}

func TestComputeScalingFactor_DefaultsToOne(t *testing.T) {
	t.Parallel()
	// Missing servings → 1.0
	if computeScalingFactor(map[string]any{}, map[string]any{}) != 1.0 {
		t.Fatal("expected 1.0 when servings missing")
	}
	// Missing recipeYield → 1.0
	if computeScalingFactor(map[string]any{"servings": float64(4)}, map[string]any{}) != 1.0 {
		t.Fatal("expected 1.0 when recipeYield missing")
	}
	// Zero yield value → 1.0
	f := computeScalingFactor(
		map[string]any{"servings": float64(4)},
		map[string]any{"recipeYield": map[string]any{"value": float64(0)}},
	)
	if f != 1.0 {
		t.Fatalf("expected 1.0 for zero yield, got %v", f)
	}
	// Normal case (nested map): 4 / 2 = 2.
	f = computeScalingFactor(
		map[string]any{"servings": float64(4)},
		map[string]any{"recipeYield": map[string]any{"value": float64(2)}},
	)
	if f != 2.0 {
		t.Fatalf("expected 2.0 (nested map), got %v", f)
	}
	// Normal case (JSON string — projection row format): 4 / 2 = 2.
	f = computeScalingFactor(
		map[string]any{"servings": float64(4)},
		map[string]any{"recipeYield": `{"value":2,"unitText":"servings"}`},
	)
	if f != 2.0 {
		t.Fatalf("expected 2.0 (JSON string), got %v", f)
	}
}

func TestSortFoodItemsByExpiration(t *testing.T) {
	t.Parallel()
	items := []map[string]any{
		{"id": "late", "expirationDate": "2026-06-01"},
		{"id": "nodate"},
		{"id": "early", "expirationDate": "2026-04-01"},
		{"id": "mid", "expirationDate": "2026-05-01"},
	}
	sortFoodItemsByExpiration(items)
	wantOrder := []string{"early", "mid", "late", "nodate"}
	for i, want := range wantOrder {
		if items[i]["id"] != want {
			t.Fatalf("position %d: want %q, got %q", i, want, items[i]["id"])
		}
	}
}
