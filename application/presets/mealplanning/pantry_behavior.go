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

	"weos/application"
	"weos/domain/entities"
	"weos/domain/repositories"
)

// enforceSingleDefaultBehavior ensures only one pantry per account has
// isDefault=true. When a new/updated pantry is marked default, every other
// default pantry gets its isDefault cleared.
type enforceSingleDefaultBehavior struct {
	baseBehavior
}

// NewEnforceSingleDefaultBehavior returns a stateless behavior instance.
// Dependencies are injected later via SetDependencies.
func NewEnforceSingleDefaultBehavior() *enforceSingleDefaultBehavior {
	return &enforceSingleDefaultBehavior{}
}

func (b *enforceSingleDefaultBehavior) AfterCreate(
	ctx context.Context, resource *entities.Resource,
) error {
	b.enforce(ctx, resource)
	return nil
}

func (b *enforceSingleDefaultBehavior) AfterUpdate(
	ctx context.Context, resource *entities.Resource,
) error {
	b.enforce(ctx, resource)
	return nil
}

// enforce unsets isDefault on all other pantries when this one is default.
// Errors are logged but never propagated so the triggering operation succeeds.
func (b *enforceSingleDefaultBehavior) enforce(
	ctx context.Context, resource *entities.Resource,
) {
	svc := b.svc()
	log := b.log()
	if svc == nil {
		return
	}

	pantry, err := extractFlatData(resource)
	if err != nil {
		if log != nil {
			log.Error(ctx, "pantry behavior: invalid data",
				"id", resource.GetID(), "error", err)
		}
		return
	}

	isDefault, _ := pantry["isDefault"].(bool)
	if !isDefault {
		return
	}

	// Find other pantries marked as default.
	filters := []repositories.FilterCondition{
		{Field: "isDefault", Operator: "eq", Value: "true"},
	}
	resp, err := svc.ListFlatWithFilters(
		ctx, "pantry", filters, "", 1000, repositories.SortOptions{},
	)
	if err != nil {
		if log != nil {
			log.Error(ctx, "pantry behavior: failed to list default pantries",
				"error", err)
		}
		return
	}

	for _, other := range resp.Data {
		otherID, _ := other["id"].(string)
		if otherID == "" || otherID == resource.GetID() {
			continue
		}
		update := map[string]any{"isDefault": false}
		// Preserve existing fields so the update doesn't clobber them.
		for k, v := range other {
			if k == "id" || k == "isDefault" {
				continue
			}
			update[k] = v
		}
		data, mErr := json.Marshal(update)
		if mErr != nil {
			continue
		}
		if _, uErr := svc.Update(ctx, application.UpdateResourceCommand{
			ID: otherID, Data: data,
		}); uErr != nil && log != nil {
			log.Error(ctx, "pantry behavior: failed to unset default",
				"id", otherID, "error", uErr)
		}
	}
}

// extractFlatData returns the resource's data as a flat map for behavior
// inspection. The underlying data is JSON-LD, so we unmarshal and return the
// first @graph entity if present, otherwise the raw object.
func extractFlatData(resource *entities.Resource) (map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal(resource.Data(), &raw); err != nil {
		return nil, err
	}
	if graph, ok := raw["@graph"].([]any); ok && len(graph) > 0 {
		if first, ok := graph[0].(map[string]any); ok {
			return first, nil
		}
	}
	return raw, nil
}
