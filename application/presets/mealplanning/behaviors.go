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

	"weos/application"
	"weos/domain/entities"
	"weos/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
)

// baseBehavior holds the BehaviorServices injected via BehaviorFactory
// at startup. All meal-planning behaviors embed this so they share the
// same dependency wiring pattern.
type baseBehavior struct {
	entities.DefaultBehavior
	writer application.ResourceWriter
	logger entities.Logger
	svc    application.BehaviorServices
}

func newBase(svc application.BehaviorServices) baseBehavior {
	return baseBehavior{
		writer: svc.Writer,
		logger: svc.Logger,
		svc:    svc,
	}
}

// visibilityScope derives a per-request VisibilityScope from the auth
// context. When auth context is absent (CLI, MCP, background jobs),
// falls back to the triggering resource's ownership so behaviors never
// query/mutate resources across tenants.
//
// Pass the triggering resource as a fallback; if both auth and resource
// are unavailable, returns a scope that matches nothing (fail closed).
func visibilityScope(
	ctx context.Context, fallback *entities.Resource,
) *repositories.VisibilityScope {
	identity := auth.AgentFromCtx(ctx)
	if identity != nil {
		return &repositories.VisibilityScope{
			AgentID:   identity.AgentID,
			AccountID: identity.ActiveAccountID,
			IsAdmin:   false,
		}
	}
	// No auth context — fall back to the resource's ownership fields.
	if fallback != nil {
		if aid := fallback.AccountID(); aid != "" {
			return &repositories.VisibilityScope{
				AgentID:   fallback.CreatedBy(),
				AccountID: aid,
				IsAdmin:   false,
			}
		}
	}
	// Fail closed: scope that cannot match any resource.
	return &repositories.VisibilityScope{
		AgentID:   "__no_identity__",
		AccountID: "__no_identity__",
		IsAdmin:   false,
	}
}

// loadFlatRow loads the flat projection row for a resource, including FK
// values for reference properties (which are stripped from the JSON-LD
// data stored in Resource.Data()). Returns nil if the resource is not found
// or has no projection row.
// loadFlatRow loads the flat projection row for a resource, including FK
// values for reference properties (which are stripped from the JSON-LD
// data stored in Resource.Data()). The fallback resource is used to
// derive a VisibilityScope when auth context is absent.
func (b *baseBehavior) loadFlatRow(
	ctx context.Context, typeSlug, id string,
	fallback *entities.Resource,
) map[string]any {
	if b.svc.Resources == nil {
		addNilSvcWarning(ctx, "loadFlatRow")
		return nil
	}
	filters := []repositories.FilterCondition{
		{Field: "id", Operator: "eq", Value: id},
	}
	resp, err := b.svc.Resources.FindAllByTypeFlatWithFilters(
		ctx, typeSlug, filters, "", 1, repositories.SortOptions{},
		visibilityScope(ctx, fallback),
	)
	if err != nil || len(resp.Data) == 0 {
		return nil
	}
	return resp.Data[0]
}
