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

package repositories

import (
	"context"
	"encoding/json"

	"weos/domain/entities"
)

// SortOptions configures sorting for list queries.
type SortOptions struct {
	SortBy    string // column name in camelCase (e.g. "submittedAt"), default "id"
	SortOrder string // "asc" or "desc", default "asc"
}

// VisibilityScope limits query results to resources the caller can access.
// When nil, no filtering is applied (system/CLI/MCP context).
type VisibilityScope struct {
	AgentID   string
	AccountID string
	IsAdmin   bool
}

type ResourceRepository interface {
	Save(ctx context.Context, entity *entities.Resource) error
	FindByID(ctx context.Context, id string) (*entities.Resource, error)
	FindAllByType(ctx context.Context, typeSlug string, cursor string, limit int,
		sort SortOptions, scope *VisibilityScope) (PaginatedResponse[*entities.Resource], error)
	FindAllByTypeAndField(ctx context.Context, typeSlug, fieldName, fieldValue string) (
		[]*entities.Resource, error)
	FindAllByTypeWithFilters(ctx context.Context, typeSlug string, filters []FilterCondition,
		cursor string, limit int, sort SortOptions, scope *VisibilityScope) (
		PaginatedResponse[*entities.Resource], error)
	Update(ctx context.Context, entity *entities.Resource) error
	// UpdateData updates the JSON-LD data for a resource, and conditionally updates
	// its sequence number (skipped when sequenceNo is 0). This is a projection-level
	// operation used by triple handlers to update the materialized @graph and keep the
	// aggregate version in sync — it does NOT emit any events.
	UpdateData(ctx context.Context, id string, data json.RawMessage, sequenceNo int) error
	Delete(ctx context.Context, id string) error

	// FindAllByTypeFlat returns flat rows from the projection table directly (no JSON-LD).
	// Used for list views where denormalized columns (including _display) are needed.
	FindAllByTypeFlat(ctx context.Context, typeSlug, cursor string, limit int,
		sort SortOptions, scope *VisibilityScope) (PaginatedResponse[map[string]any], error)
	// FindAllByTypeFlatWithFilters returns filtered flat rows from the projection table.
	FindAllByTypeFlatWithFilters(ctx context.Context, typeSlug string, filters []FilterCondition,
		cursor string, limit int, sort SortOptions, scope *VisibilityScope) (
		PaginatedResponse[map[string]any], error)
}
