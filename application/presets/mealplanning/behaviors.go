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
// context. Behaviors must scope queries to the current user's resources
// in multi-user environments. Returns nil for system contexts (CLI/MCP
// without auth), which disables visibility filtering.
func visibilityScope(ctx context.Context) *repositories.VisibilityScope {
	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return nil
	}
	return &repositories.VisibilityScope{
		AgentID:   identity.AgentID,
		AccountID: identity.ActiveAccountID,
		IsAdmin:   false,
	}
}

// loadFlatRow loads the flat projection row for a resource, including FK
// values for reference properties (which are stripped from the JSON-LD
// data stored in Resource.Data()). Returns nil if the resource is not found
// or has no projection row.
func (b *baseBehavior) loadFlatRow(
	ctx context.Context, typeSlug, id string,
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
		visibilityScope(ctx),
	)
	if err != nil || len(resp.Data) == 0 {
		return nil
	}
	return resp.Data[0]
}
