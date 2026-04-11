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
	"time"

	"weos/domain/entities"
)

func setupScheduledMealBehavior(t *testing.T) (*scheduledMealBehavior, *stubResourceSvc) {
	t.Helper()
	stub := newStubResourceSvc()
	b := newScheduledMealBehavior(testBehaviorServices(stub))
	return b, stub
}

// scheduledMealTestBaseNow is captured once so all helpers use the same
// UTC day, avoiding flakiness if the test suite runs across midnight.
var scheduledMealTestBaseNow = time.Now().UTC().Truncate(24 * time.Hour)

func futureDate(days int) string {
	return scheduledMealTestBaseNow.AddDate(0, 0, days).Format("2006-01-02")
}

func pastDate(days int) string {
	return scheduledMealTestBaseNow.AddDate(0, 0, -days).Format("2006-01-02")
}

func TestGenerateOccurrences_AfterCreate_CreatesForEachDate(t *testing.T) {
	t.Parallel()
	b, stub := setupScheduledMealBehavior(t)

	// Schedule 3 daily meals starting from a fixed past date so walker runs
	// in non-onlyFuture mode and emits all 3.
	resource := makeTestResource(t, "sm-1", "scheduled-meal", map[string]any{
		"startDate":       "2026-04-10",
		"repeatFrequency": "P1D",
		"repeatCount":     float64(3),
		"mealType":        "dinner",
		"servings":        float64(4),
	})

	if err := b.AfterCreate(context.Background(), resource); err != nil {
		t.Fatalf("AfterCreate returned error: %v", err)
	}

	if len(stub.creates) != 3 {
		t.Fatalf("expected 3 meal-occurrence creates, got %d", len(stub.creates))
	}
	for _, c := range stub.creates {
		if c.TypeSlug != "meal-occurrence" {
			t.Fatalf("unexpected typeSlug: %q", c.TypeSlug)
		}
		var m map[string]any
		if err := json.Unmarshal(c.Data, &m); err != nil {
			t.Fatalf("failed to unmarshal create payload: %v; data=%s", err, c.Data)
		}
		if m["status"] != "planned" {
			t.Fatalf("expected status=planned, got %v", m["status"])
		}
		if m["mealType"] != "dinner" {
			t.Fatalf("expected mealType=dinner, got %v", m["mealType"])
		}
		if toFloat(m["servings"]) != 4 {
			t.Fatalf("expected servings=4, got %v", m["servings"])
		}
		if m["scheduledMeal"] != "sm-1" {
			t.Fatalf("expected scheduledMeal=sm-1, got %v", m["scheduledMeal"])
		}
	}
}

func TestGenerateOccurrences_ExpansionErrorSurfacesWarning(t *testing.T) {
	t.Parallel()
	b, _ := setupScheduledMealBehavior(t)
	resource := makeTestResource(t, "sm-1", "scheduled-meal", map[string]any{
		"startDate":       "2026-04-10",
		"repeatFrequency": "BAD",
		"mealType":        "dinner",
	})
	ctx := entities.ContextWithMessages(context.Background())
	_ = b.AfterCreate(ctx, resource)

	found := false
	for _, m := range entities.GetMessages(ctx) {
		if m.Code == "scheduled_meal_expansion_error" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected scheduled_meal_expansion_error warning")
	}
}

func TestGenerateOccurrences_CapHitEmitsWarning(t *testing.T) {
	t.Parallel()
	b, _ := setupScheduledMealBehavior(t)
	resource := makeTestResource(t, "sm-1", "scheduled-meal", map[string]any{
		"startDate":       "2026-01-01",
		"endDate":         "2028-01-01", // ~730 days
		"repeatFrequency": "P1D",
		"mealType":        "dinner",
	})
	ctx := entities.ContextWithMessages(context.Background())
	_ = b.AfterCreate(ctx, resource)

	found := false
	for _, m := range entities.GetMessages(ctx) {
		if m.Code == "scheduled_meal_occurrence_cap_reached" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected scheduled_meal_occurrence_cap_reached warning")
	}
}

func TestRegenerate_PreservesPastAndCookedSkipped(t *testing.T) {
	t.Parallel()
	b, stub := setupScheduledMealBehavior(t)

	// Pre-seed existing occurrences with mixed statuses and dates.
	stub.listFlatData["meal-occurrence"] = []map[string]any{
		{"id": "occ-past-planned", "date": pastDate(3), "status": "planned",
			"scheduledMeal": "sm-1"},
		{"id": "occ-past-cooked", "date": pastDate(2), "status": "cooked",
			"scheduledMeal": "sm-1"},
		{"id": "occ-future-planned", "date": futureDate(3), "status": "planned",
			"scheduledMeal": "sm-1"},
		{"id": "occ-future-skipped", "date": futureDate(4), "status": "skipped",
			"scheduledMeal": "sm-1"},
		{"id": "occ-future-cooked", "date": futureDate(5), "status": "cooked",
			"scheduledMeal": "sm-1"},
	}

	// Update triggers regenerate with a non-recurring schedule on futureDate(10).
	resource := makeTestResource(t, "sm-1", "scheduled-meal", map[string]any{
		"startDate": futureDate(10),
		"mealType":  "dinner",
	})
	if err := b.AfterUpdate(context.Background(), resource); err != nil {
		t.Fatalf("AfterUpdate returned error: %v", err)
	}

	// Only the one future-planned occurrence should be deleted.
	if len(stub.deletes) != 1 {
		t.Fatalf("expected 1 delete, got %d: %v", len(stub.deletes), stub.deletes)
	}
	if stub.deletes[0].ID != "occ-future-planned" {
		t.Fatalf("expected delete of occ-future-planned, got %v", stub.deletes[0].ID)
	}
	// One new occurrence created on futureDate(10).
	if len(stub.creates) != 1 {
		t.Fatalf("expected 1 create, got %d", len(stub.creates))
	}
}

func TestRegenerate_ExpansionErrorPreservesExisting(t *testing.T) {
	t.Parallel()
	b, stub := setupScheduledMealBehavior(t)
	stub.listFlatData["meal-occurrence"] = []map[string]any{
		{"id": "occ-future", "date": futureDate(3), "status": "planned",
			"scheduledMeal": "sm-1"},
	}

	// Malformed schedule: bad repeatFrequency.
	resource := makeTestResource(t, "sm-1", "scheduled-meal", map[string]any{
		"startDate":       futureDate(1),
		"repeatFrequency": "BAD",
		"mealType":        "dinner",
	})
	ctx := entities.ContextWithMessages(context.Background())
	_ = b.AfterUpdate(ctx, resource)

	if len(stub.deletes) != 0 {
		t.Fatalf("expected no deletes when expansion fails, got %d", len(stub.deletes))
	}
	if len(stub.creates) != 0 {
		t.Fatalf("expected no creates when expansion fails, got %d", len(stub.creates))
	}
	found := false
	for _, m := range entities.GetMessages(ctx) {
		if m.Code == "scheduled_meal_regenerate_expansion_error" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected regenerate expansion error warning")
	}
}

func TestCascadeDelete_PreservesHistory(t *testing.T) {
	t.Parallel()
	b, stub := setupScheduledMealBehavior(t)
	stub.listFlatData["meal-occurrence"] = []map[string]any{
		{"id": "occ-past-planned", "date": pastDate(3), "status": "planned",
			"scheduledMeal": "sm-1"},
		{"id": "occ-past-cooked", "date": pastDate(2), "status": "cooked",
			"scheduledMeal": "sm-1"},
		{"id": "occ-future-planned", "date": futureDate(3), "status": "planned",
			"scheduledMeal": "sm-1"},
		{"id": "occ-future-skipped", "date": futureDate(4), "status": "skipped",
			"scheduledMeal": "sm-1"},
	}

	resource := makeTestResource(t, "sm-1", "scheduled-meal", map[string]any{
		"startDate": futureDate(1),
		"mealType":  "dinner",
	})
	if err := b.AfterDelete(context.Background(), resource); err != nil {
		t.Fatalf("AfterDelete returned error: %v", err)
	}

	// Only the future planned occurrence should be deleted.
	if len(stub.deletes) != 1 {
		t.Fatalf("expected 1 delete, got %d", len(stub.deletes))
	}
	if stub.deletes[0].ID != "occ-future-planned" {
		t.Fatalf("expected delete of occ-future-planned, got %v", stub.deletes[0].ID)
	}
}

func TestListOccurrences_PropagatesError(t *testing.T) {
	t.Parallel()
	b, stub := setupScheduledMealBehavior(t)
	stub.listFlatErr["meal-occurrence"] = errTestBoom

	ctx := entities.ContextWithMessages(context.Background())
	resource := makeTestResource(t, "sm-1", "scheduled-meal", map[string]any{
		"startDate": futureDate(1),
		"mealType":  "dinner",
	})
	_ = b.AfterDelete(ctx, resource)

	found := false
	for _, m := range entities.GetMessages(ctx) {
		if m.Code == "scheduled_meal_cascade_list_error" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected cascade list error warning")
	}
}

// errTestBoom is a canned error for tests.
var errTestBoom = &testError{"boom"}

type testError struct{ s string }

func (e *testError) Error() string { return e.s }
