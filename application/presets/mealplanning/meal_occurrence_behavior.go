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

	"weos/application"
	"weos/domain/entities"
	"weos/domain/repositories"
)

// depletePantryOnCookBehavior decrements FoodItem quantities in the target
// pantry when a MealOccurrence transitions to status=cooked.
type depletePantryOnCookBehavior struct {
	baseBehavior
}

// NewDepletePantryOnCookBehavior returns a stateless behavior instance.
func NewDepletePantryOnCookBehavior() *depletePantryOnCookBehavior {
	return &depletePantryOnCookBehavior{}
}

func (b *depletePantryOnCookBehavior) AfterUpdate(
	ctx context.Context, resource *entities.Resource,
) error {
	b.deplete(ctx, resource)
	return nil
}

func (b *depletePantryOnCookBehavior) deplete(
	ctx context.Context, resource *entities.Resource,
) {
	svc := b.svc()
	log := b.log()
	if svc == nil {
		return
	}

	occurrence, err := extractFlatData(resource)
	if err != nil {
		if log != nil {
			log.Error(ctx, "depletion: invalid occurrence data",
				"id", resource.GetID(), "error", err)
		}
		return
	}

	status, _ := occurrence["status"].(string)
	if status != "cooked" {
		return
	}

	scheduledMealID, _ := occurrence["scheduledMeal"].(string)
	if scheduledMealID == "" {
		return
	}

	// Resolve ScheduledMeal → Recipe → RecipeIngredients.
	scheduledMeal, err := svc.GetByID(ctx, scheduledMealID)
	if err != nil {
		if log != nil {
			log.Error(ctx, "depletion: failed to load scheduled meal",
				"id", scheduledMealID, "error", err)
		}
		return
	}
	smData, err := extractFlatData(scheduledMeal)
	if err != nil {
		return
	}
	recipeID, _ := smData["recipe"].(string)
	if recipeID == "" {
		return
	}

	recipe, err := svc.GetByID(ctx, recipeID)
	if err != nil {
		if log != nil {
			log.Error(ctx, "depletion: failed to load recipe",
				"id", recipeID, "error", err)
		}
		return
	}
	recipeData, err := extractFlatData(recipe)
	if err != nil {
		return
	}

	// Compute scaling factor: occurrence servings / recipe yield value.
	scale := computeScalingFactor(occurrence, recipeData)

	// Load RecipeIngredient resources linked to this recipe.
	ingredientFilters := []repositories.FilterCondition{
		{Field: "recipe", Operator: "eq", Value: recipeID},
	}
	riResp, err := svc.ListFlatWithFilters(
		ctx, "recipe-ingredient", ingredientFilters, "", 500,
		repositories.SortOptions{},
	)
	if err != nil {
		if log != nil {
			log.Error(ctx, "depletion: failed to list recipe ingredients",
				"error", err)
		}
		return
	}

	// Resolve target pantry.
	pantryID := b.resolvePantry(ctx, smData)
	if pantryID == "" {
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
		return
	}
	neededQty := toFloat(ri["quantity"]) * scale
	neededUnit, _ := ri["unit"].(string)
	if neededQty <= 0 {
		return
	}

	// Find FoodItems in target pantry matching this ingredient.
	filters := []repositories.FilterCondition{
		{Field: "pantry", Operator: "eq", Value: pantryID},
		{Field: "ingredient", Operator: "eq", Value: ingredientID},
	}
	resp, err := svc.ListFlatWithFilters(
		ctx, "food-item", filters, "", 500, repositories.SortOptions{},
	)
	if err != nil {
		if log != nil {
			log.Error(ctx, "depletion: failed to list food items",
				"ingredient", ingredientID, "error", err)
		}
		return
	}
	if len(resp.Data) == 0 {
		return
	}

	// Sort by expirationDate ascending (FIFO by expiry).
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

// updateFoodItemQuantity issues an update with the new quantity, preserving
// other fields.
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
	for k, v := range fi {
		if k == "id" || k == "quantity" {
			continue
		}
		update[k] = v
	}
	data, err := json.Marshal(update)
	if err != nil {
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
	if svc == nil {
		return ""
	}
	mealPlanID, _ := scheduledMealData["mealPlan"].(string)
	if mealPlanID != "" {
		mealPlan, err := svc.GetByID(ctx, mealPlanID)
		if err == nil {
			mpData, _ := extractFlatData(mealPlan)
			if mpData != nil {
				if p, _ := mpData["pantry"].(string); p != "" {
					return p
				}
			}
		}
	}
	// Fall back to default pantry in the account.
	filters := []repositories.FilterCondition{
		{Field: "isDefault", Operator: "eq", Value: "true"},
	}
	resp, err := svc.ListFlatWithFilters(
		ctx, "pantry", filters, "", 1, repositories.SortOptions{},
	)
	if err != nil || len(resp.Data) == 0 {
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
			return false
		}
		if ej == "" {
			return true
		}
		return ei < ej
	})
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		return 0
	}
	return 0
}
