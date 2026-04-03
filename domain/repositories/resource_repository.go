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

	"weos/domain/entities"
)

// SortOptions configures sorting for list queries.
type SortOptions struct {
	SortBy    string // column name in camelCase (e.g. "submittedAt"), default "id"
	SortOrder string // "asc" or "desc", default "asc"
}

type ResourceRepository interface {
	Save(ctx context.Context, entity *entities.Resource) error
	FindByID(ctx context.Context, id string) (*entities.Resource, error)
	FindAllByType(ctx context.Context, typeSlug string, cursor string, limit int, sort SortOptions) (
		PaginatedResponse[*entities.Resource], error)
	FindAllByTypeWithFilters(ctx context.Context, typeSlug string, filters []FilterCondition,
		cursor string, limit int, sort SortOptions) (PaginatedResponse[*entities.Resource], error)
	Update(ctx context.Context, entity *entities.Resource) error
	Delete(ctx context.Context, id string) error
}
