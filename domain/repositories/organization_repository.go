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

type OrganizationRepository interface {
	Save(ctx context.Context, entity *entities.Organization) error
	FindByID(ctx context.Context, id string) (*entities.Organization, error)
	FindAll(ctx context.Context, cursor string, limit int) (PaginatedResponse[*entities.Organization], error)
	Update(ctx context.Context, entity *entities.Organization) error
	Delete(ctx context.Context, id string) error
}
