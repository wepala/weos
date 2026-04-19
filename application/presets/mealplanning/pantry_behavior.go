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
	"strconv"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// enforceSingleDefaultBehavior ensures only one pantry in the acting user's
// visibility scope has isDefault=true. When a new/updated pantry is marked
// default, every other visible default pantry gets its isDefault cleared.
type enforceSingleDefaultBehavior struct {
	baseBehavior
}

// newEnforceSingleDefaultBehavior returns a behavior instance wired with the
// given services. Called by the BehaviorFactory registered in the preset.
func newEnforceSingleDefaultBehavior(svc application.BehaviorServices) *enforceSingleDefaultBehavior {
	return &enforceSingleDefaultBehavior{baseBehavior: newBase(svc)}
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

// pantryWriteFields are the Pantry schema fields safe to echo back in an
// update payload. System fields are excluded.
var pantryWriteFields = []string{
	"name", "description", "location", "isDefault",
}

// enforce unsets isDefault on all other pantries when this one is default.
// Enforcement is best-effort: failures are logged, and some paths emit
// warnings, but the triggering operation is still allowed to succeed.
func (b *enforceSingleDefaultBehavior) enforce(
	ctx context.Context, resource *entities.Resource,
) {
	if b.writer == nil || b.svc.Resources == nil {
		addNilSvcWarning(ctx, "pantry enforcement")
		return
	}

	pantry, err := extractFlatDataByID(resource, resource.GetID())
	if err != nil {
		if b.logger != nil {
			b.logger.Error(ctx, "pantry behavior: invalid data",
				"id", resource.GetID(), "error", err)
		}
		return
	}

	isDefault, _ := pantry["isDefault"].(bool)
	if !isDefault {
		return
	}

	// Use "1" as the portable boolean representation: SQLite stores BOOLEAN
	// as INTEGER 0/1, and PostgreSQL coerces "1" to true. String "true"
	// fails to match in SQLite.
	filters := []repositories.FilterCondition{
		{Field: "isDefault", Operator: "eq", Value: "1"},
	}
	scope := visibilityScope(ctx, resource)
	const pageSize = 100
	cursor := ""
	for {
		resp, err := b.svc.Resources.FindAllByTypeFlatWithFilters(
			ctx, "pantry", filters, cursor, pageSize,
			repositories.SortOptions{}, scope,
		)
		if err != nil {
			addServiceErrorMessage(ctx, b.logger,
				"pantry behavior: failed to list default pantries",
				"Failed to list default pantries; single-default invariant not enforced.",
				"pantry_default_list_error",
				"error", err)
			return
		}

		for _, other := range resp.Data {
			otherID, _ := other["id"].(string)
			if otherID == "" || otherID == resource.GetID() {
				continue
			}
			update := map[string]any{"isDefault": false}
			for _, field := range pantryWriteFields {
				if field == "isDefault" {
					continue
				}
				if v, ok := other[field]; ok && v != nil {
					update[field] = v
				}
			}
			data, mErr := json.Marshal(update)
			if mErr != nil {
				if b.logger != nil {
					b.logger.Error(ctx, "pantry behavior: failed to marshal update",
						"id", otherID, "error", mErr)
				}
				continue
			}
			if _, uErr := b.writer.Update(ctx, application.UpdateResourceCommand{
				ID: otherID, Data: data,
			}); uErr != nil && b.logger != nil {
				b.logger.Error(ctx, "pantry behavior: failed to unset default",
					"id", otherID, "error", uErr)
			}
		}

		if resp.Cursor == "" || len(resp.Data) < pageSize {
			break
		}
		cursor = resp.Cursor
	}
}

// -- shared helpers (used by all meal-planning behaviors) -------------------

// extractFlatData returns the first flat entity from a resource's JSON-LD
// data, falling back to the raw object. Provided for callers that don't
// know the target ID; prefer extractFlatDataByID when the ID is known.
func extractFlatData(resource *entities.Resource) (map[string]any, error) {
	return extractFlatDataByID(resource, resource.GetID())
}

// extractFlatDataByID unmarshals a resource's JSON-LD data and returns the
// entity matching the given ID (via @id), falling back to the first entry
// in @graph, then to the raw object.
func extractFlatDataByID(resource *entities.Resource, id string) (map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal(resource.Data(), &raw); err != nil {
		return nil, err
	}
	graph, ok := raw["@graph"].([]any)
	if !ok || len(graph) == 0 {
		return raw, nil
	}
	// Try to find the entry whose @id matches the given ID.
	if id != "" {
		for _, e := range graph {
			m, ok := e.(map[string]any)
			if !ok {
				continue
			}
			if atID, _ := m["@id"].(string); atID == id {
				return m, nil
			}
		}
	}
	// Fall back to the first graph entry.
	if first, ok := graph[0].(map[string]any); ok {
		return first, nil
	}
	return raw, nil
}

// addNilSvcWarning surfaces a user-facing warning when a behavior fires
// without required dependencies injected. This makes dependency
// misconfiguration visible instead of silently no-opping.
func addNilSvcWarning(ctx context.Context, where string) {
	entities.AddMessage(ctx, entities.Message{
		Type: "warning",
		Text: "Behavior misconfiguration: " + where +
			" — required behavior dependencies are missing; no action taken.",
		Code: "behavior_dependency_missing",
	})
}

// addServiceErrorMessage logs the given error and adds a user-facing warning
// with a stable code so UI consumers can react. This is the standard pattern
// for surfacing silent failures in behavior post-commit hooks.
func addServiceErrorMessage(
	ctx context.Context, log entities.Logger,
	logMsg, userMsg, code string, kv ...any,
) {
	if log != nil {
		log.Error(ctx, logMsg, kv...)
	}
	entities.AddMessage(ctx, entities.Message{
		Type: "warning",
		Text: userMsg,
		Code: code,
	})
}

// toFloat converts a JSON-decoded value to float64. Handles numeric types
// and string numeric representations (which can appear in JSON-LD payloads).
func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float32:
		return float64(n)
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f
		}
	}
	return 0
}
