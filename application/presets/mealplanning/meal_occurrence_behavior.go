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
	"fmt"
	"sort"
	"sync"

	"weos/application"
	"weos/domain/entities"
	"weos/domain/repositories"
)

// depletePantryOnCookBehavior decrements FoodItem quantities in the target
// pantry when a MealOccurrence transitions from non-cooked to cooked.
//
// Re-depletion guard: BeforeUpdate compares the existing status against the
// incoming data and marks the occurrence ID as "pending depletion" in an
// internal map. AfterUpdate only runs depletion if the marker is set, then
// removes it. This ensures editing a field on an already-cooked occurrence
// (e.g. fixing notes) does NOT re-deplete the pantry.
type depletePantryOnCookBehavior struct {
	baseBehavior
	pendingMu sync.Mutex
	pending   map[string]struct{}
}

// NewDepletePantryOnCookBehavior returns a stateless behavior instance.
func NewDepletePantryOnCookBehavior() *depletePantryOnCookBehavior {
	return &depletePantryOnCookBehavior{
		pending: make(map[string]struct{}),
	}
}

// BeforeUpdate detects the status transition from non-cooked → cooked and
// stashes the resource ID so AfterUpdate knows to run depletion exactly once.
// Any previous marker for the same ID is cleared first so a failed-then-
// retried update doesn't carry a stale marker forward.
func (b *depletePantryOnCookBehavior) BeforeUpdate(
	_ context.Context, existing *entities.Resource,
	data json.RawMessage, _ *entities.ResourceType,
) (json.RawMessage, error) {
	if existing == nil {
		return data, nil
	}
	// Clear any stale marker first (in case a prior update failed after
	// BeforeUpdate but before AfterUpdate could consume it).
	b.clearPending(existing.GetID())

	prev, err := extractFlatData(existing)
	if err != nil {
		return data, nil //nolint:nilerr // behavior must not block the update
	}
	var next map[string]any
	if err := json.Unmarshal(data, &next); err != nil {
		return data, nil //nolint:nilerr
	}
	prevStatus, _ := prev["status"].(string)
	nextStatus, _ := next["status"].(string)
	if nextStatus == "cooked" && prevStatus != "cooked" {
		b.markPending(existing.GetID())
	}
	return data, nil
}

func (b *depletePantryOnCookBehavior) AfterUpdate(
	ctx context.Context, resource *entities.Resource,
) error {
	if !b.takePending(resource.GetID()) {
		return nil // not a transition → cooked, skip
	}
	b.deplete(ctx, resource)
	return nil
}

func (b *depletePantryOnCookBehavior) markPending(id string) {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	b.pending[id] = struct{}{}
}

// clearPending removes any marker for id. Called at the start of
// BeforeUpdate to prevent stale markers from leaking across failed updates.
func (b *depletePantryOnCookBehavior) clearPending(id string) {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	delete(b.pending, id)
}

// takePending atomically checks and clears the pending marker for id.
// Returns true if the marker was set (and was just cleared).
func (b *depletePantryOnCookBehavior) takePending(id string) bool {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	_, ok := b.pending[id]
	if ok {
		delete(b.pending, id)
	}
	return ok
}

func (b *depletePantryOnCookBehavior) deplete(
	ctx context.Context, resource *entities.Resource,
) {
	svc := b.svc()
	log := b.log()
	if svc == nil {
		addNilSvcWarning(ctx, "meal-occurrence depletion")
		return
	}

	occurrence, err := extractFlatDataByID(resource, resource.GetID())
	if err != nil {
		if log != nil {
			log.Error(ctx, "depletion: invalid occurrence data",
				"id", resource.GetID(), "error", err)
		}
		return
	}

	// Defensive: even if pending marker was set, re-check status.
	// (Guards against stale markers if BeforeUpdate ran but AfterUpdate
	// fires on a later update where status was reverted before marker consumed.)
	status, _ := occurrence["status"].(string)
	if status != "cooked" {
		return
	}

	scheduledMealID, _ := occurrence["scheduledMeal"].(string)
	if scheduledMealID == "" {
		entities.AddMessage(ctx, entities.Message{
			Type: "warning",
			Text: "Cooked meal occurrence has no scheduled meal reference; pantry not depleted.",
			Code: "pantry_depletion_no_scheduled_meal",
		})
		return
	}

	scheduledMeal, err := svc.GetByID(ctx, scheduledMealID)
	if err != nil {
		addServiceErrorMessage(ctx, log,
			"depletion: failed to load scheduled meal",
			"scheduled meal could not be loaded; pantry not depleted",
			"pantry_depletion_scheduled_meal_error",
			"id", scheduledMealID, "error", err)
		return
	}
	smData, err := extractFlatDataByID(scheduledMeal, scheduledMealID)
	if err != nil {
		if log != nil {
			log.Error(ctx, "depletion: invalid scheduled meal data",
				"id", scheduledMealID, "error", err)
		}
		return
	}
	recipeID, _ := smData["recipe"].(string)
	if recipeID == "" {
		entities.AddMessage(ctx, entities.Message{
			Type: "warning",
			Text: "Scheduled meal has no recipe reference; pantry not depleted.",
			Code: "pantry_depletion_no_recipe",
		})
		return
	}

	recipe, err := svc.GetByID(ctx, recipeID)
	if err != nil {
		addServiceErrorMessage(ctx, log,
			"depletion: failed to load recipe",
			"recipe could not be loaded; pantry not depleted",
			"pantry_depletion_recipe_error",
			"id", recipeID, "error", err)
		return
	}
	recipeData, err := extractFlatDataByID(recipe, recipeID)
	if err != nil {
		if log != nil {
			log.Error(ctx, "depletion: invalid recipe data",
				"id", recipeID, "error", err)
		}
		return
	}

	scale := computeScalingFactor(occurrence, recipeData)

	ingredientFilters := []repositories.FilterCondition{
		{Field: "recipe", Operator: "eq", Value: recipeID},
	}
	riResp, err := svc.ListFlatWithFilters(
		ctx, "recipe-ingredient", ingredientFilters, "", 500,
		repositories.SortOptions{},
	)
	if err != nil {
		addServiceErrorMessage(ctx, log,
			"depletion: failed to list recipe ingredients",
			"failed to load recipe ingredients; pantry not depleted",
			"pantry_depletion_ingredient_list_error",
			"error", err)
		return
	}

	pantryID := b.resolvePantry(ctx, smData)
	if pantryID == "" {
		entities.AddMessage(ctx, entities.Message{
			Type: "warning",
			Text: "No target pantry could be resolved; pantry not depleted.",
			Code: "pantry_depletion_no_pantry",
		})
		if log != nil {
			log.Warn(ctx, "depletion: no target pantry resolved",
				"occurrence", resource.GetID())
		}
		return
	}

	for _, ri := range riResp.Data {
		b.depleteIngredient(ctx, ri, pantryID, scale)
	}
}

// depleteIngredient decrements FoodItem quantities for a single RecipeIngredient.
func (b *depletePantryOnCookBehavior) depleteIngredient(
	ctx context.Context, ri map[string]any, pantryID string, scale float64,
) {
	svc := b.svc()
	log := b.log()

	ingredientID, _ := ri["ingredient"].(string)
	if ingredientID == "" {
		if log != nil {
			log.Warn(ctx, "depletion: recipe ingredient missing ingredient ref",
				"recipeIngredient", ri["id"])
		}
		return
	}
	neededQty := toFloat(ri["quantity"]) * scale
	neededUnit, _ := ri["unit"].(string)
	if neededQty <= 0 {
		return
	}

	filters := []repositories.FilterCondition{
		{Field: "pantry", Operator: "eq", Value: pantryID},
		{Field: "ingredient", Operator: "eq", Value: ingredientID},
	}
	resp, err := svc.ListFlatWithFilters(
		ctx, "food-item", filters, "", 500, repositories.SortOptions{},
	)
	if err != nil {
		addServiceErrorMessage(ctx, log,
			"depletion: failed to list food items",
			fmt.Sprintf("failed to load food items for %q; pantry not depleted",
				ingredientID),
			"pantry_depletion_food_item_list_error",
			"ingredient", ingredientID, "error", err)
		return
	}
	if len(resp.Data) == 0 {
		// No matching food items means full shortfall.
		entities.AddMessage(ctx, entities.Message{
			Type: "warning",
			Text: fmt.Sprintf(
				"Pantry shortfall for %q: %.2f %s needed, none on hand",
				ingredientID, neededQty, neededUnit),
			Code: "pantry_depletion_shortfall",
		})
		return
	}

	sortFoodItemsByExpiration(resp.Data)

	remaining := neededQty
	for _, fi := range resp.Data {
		if remaining <= 0 {
			break
		}
		fiUnit, _ := fi["unit"].(string)
		if fiUnit != neededUnit {
			entities.AddMessage(ctx, entities.Message{
				Type: "warning",
				Text: fmt.Sprintf(
					"Unit mismatch depleting %q: recipe=%s, pantry=%s — skipped",
					ingredientID, neededUnit, fiUnit),
				Code: "pantry_depletion_unit_mismatch",
			})
			continue
		}
		available := toFloat(fi["quantity"])
		deduct := remaining
		if deduct > available {
			deduct = available
		}
		newQty := available - deduct
		remaining -= deduct

		b.updateFoodItemQuantity(ctx, fi, newQty)
	}

	if remaining > 0 {
		entities.AddMessage(ctx, entities.Message{
			Type: "warning",
			Text: fmt.Sprintf(
				"Pantry shortfall for %q: %.2f %s still needed",
				ingredientID, remaining, neededUnit),
			Code: "pantry_depletion_shortfall",
		})
	}
}

// foodItemWriteFields are the FoodItem schema fields that are safe to echo
// back in an update payload. System fields (id, createdAt, updatedAt,
// sequence_no, type_slug, etc.) are deliberately excluded.
var foodItemWriteFields = []string{
	"quantity", "unit", "storage", "purchaseDate", "expirationDate",
	"notes", "ingredient", "pantry",
}

// updateFoodItemQuantity issues an update with the new quantity, preserving
// schema-defined fields only (no system columns).
func (b *depletePantryOnCookBehavior) updateFoodItemQuantity(
	ctx context.Context, fi map[string]any, newQty float64,
) {
	svc := b.svc()
	log := b.log()
	id, _ := fi["id"].(string)
	if id == "" {
		return
	}
	update := map[string]any{"quantity": newQty}
	for _, field := range foodItemWriteFields {
		if field == "quantity" {
			continue
		}
		if v, ok := fi[field]; ok && v != nil {
			update[field] = v
		}
	}
	data, err := json.Marshal(update)
	if err != nil {
		if log != nil {
			log.Error(ctx, "depletion: failed to marshal food item update",
				"id", id, "error", err)
		}
		return
	}
	if _, uErr := svc.Update(ctx, application.UpdateResourceCommand{
		ID: id, Data: data,
	}); uErr != nil && log != nil {
		log.Error(ctx, "depletion: failed to update food item",
			"id", id, "error", uErr)
	}
}

// resolvePantry returns the pantry ID to deplete from. Uses the scheduled
// meal's mealPlan.pantry if set, otherwise the user's default pantry.
func (b *depletePantryOnCookBehavior) resolvePantry(
	ctx context.Context, scheduledMealData map[string]any,
) string {
	svc := b.svc()
	log := b.log()
	if svc == nil {
		return ""
	}
	mealPlanID, _ := scheduledMealData["mealPlan"].(string)
	if mealPlanID != "" {
		mealPlan, err := svc.GetByID(ctx, mealPlanID)
		if err != nil {
			if log != nil {
				log.Error(ctx, "depletion: failed to load meal plan",
					"id", mealPlanID, "error", err)
			}
		} else if mealPlan != nil {
			mpData, mpErr := extractFlatDataByID(mealPlan, mealPlanID)
			if mpErr == nil && mpData != nil {
				if p, _ := mpData["pantry"].(string); p != "" {
					return p
				}
			}
		}
	}
	// Fall back to default pantry in the account.
	// Portable boolean filter: "1" works on both SQLite (INTEGER 0/1) and
	// PostgreSQL (coerced to true).
	filters := []repositories.FilterCondition{
		{Field: "isDefault", Operator: "eq", Value: "1"},
	}
	resp, err := svc.ListFlatWithFilters(
		ctx, "pantry", filters, "", 1, repositories.SortOptions{},
	)
	if err != nil {
		if log != nil {
			log.Error(ctx, "depletion: failed to list default pantries",
				"error", err)
		}
		return ""
	}
	if len(resp.Data) == 0 {
		return ""
	}
	id, _ := resp.Data[0]["id"].(string)
	return id
}

// computeScalingFactor returns occurrence_servings / recipe_yield_value,
// defaulting to 1.0 when either is missing.
func computeScalingFactor(occurrence, recipe map[string]any) float64 {
	occServings := toFloat(occurrence["servings"])
	if occServings <= 0 {
		return 1.0
	}
	yield, ok := recipe["recipeYield"].(map[string]any)
	if !ok {
		return 1.0
	}
	yieldValue := toFloat(yield["value"])
	if yieldValue <= 0 {
		return 1.0
	}
	return occServings / yieldValue
}

// sortFoodItemsByExpiration sorts items with earlier expirationDate first.
// Items without expirationDate sort to the end.
func sortFoodItemsByExpiration(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		ei, _ := items[i]["expirationDate"].(string)
		ej, _ := items[j]["expirationDate"].(string)
		if ei == "" {
			return false // i without date goes after j
		}
		if ej == "" {
			return true // j without date goes after i
		}
		return ei < ej
	})
}
