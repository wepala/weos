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
	"testing"
)

// A meal occurrence does not need a schedule behind it. Someone eats a meal
// they never planned, and the model has to be able to say so — before this,
// `scheduledMeal` was required, so an ad-hoc meal could not be recorded as an
// occurrence at all and had to be minted as a separate type elsewhere.
//
// Relaxing the requirement does not weaken the scheduled path: the expansion
// behavior sets `scheduledMeal` on every occurrence it creates, and that is
// pinned by the orchestration tests. What changes is only that the schema
// stops refusing an occurrence which legitimately has no schedule.

func mealOccurrenceRequired(t *testing.T) []string {
	t.Helper()
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(mealOccurrenceType().Schema, &schema); err != nil {
		t.Fatalf("meal-occurrence schema is not valid JSON: %v", err)
	}
	return schema.Required
}

func TestMealOccurrenceDoesNotRequireASchedule(t *testing.T) {
	for _, name := range mealOccurrenceRequired(t) {
		if name == "scheduledMeal" {
			t.Error("meal-occurrence still requires scheduledMeal, so a meal eaten ad hoc cannot be recorded as an occurrence")
		}
	}
}

func TestMealOccurrenceStillRequiresWhatIdentifiesIt(t *testing.T) {
	// Relaxing one field must not turn into relaxing the type. A date, a meal
	// type and a status are what make an occurrence a fact rather than a note.
	want := map[string]bool{"date": true, "mealType": true, "status": true}
	got := map[string]bool{}
	for _, name := range mealOccurrenceRequired(t) {
		got[name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("meal-occurrence no longer requires %q; an occurrence without it is not identifiable", name)
		}
	}
}

func TestMealOccurrenceStillOffersTheScheduleLink(t *testing.T) {
	// Optional is not absent. The property and its `mp:occurrenceOf` term must
	// survive, or every occurrence expanded from a schedule loses its edge home.
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(mealOccurrenceType().Schema, &schema); err != nil {
		t.Fatalf("meal-occurrence schema is not valid JSON: %v", err)
	}
	if _, ok := schema.Properties["scheduledMeal"]; !ok {
		t.Fatal("meal-occurrence no longer declares scheduledMeal at all; the schedule link must stay available, just not mandatory")
	}

	var ctx map[string]any
	if err := json.Unmarshal(mealOccurrenceType().Context, &ctx); err != nil {
		t.Fatalf("meal-occurrence @context is not valid JSON: %v", err)
	}
	if ctx["scheduledMeal"] != "mp:occurrenceOf" {
		t.Errorf("scheduledMeal must keep its mp:occurrenceOf term, got %v", ctx["scheduledMeal"])
	}
}
