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

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/infrastructure/models"
)

// RoleResourceAccessRepository manages the role-to-resource-type access configuration.
type RoleResourceAccessRepository interface {
	Get(ctx context.Context) (*models.RoleResourceAccess, error)
	Save(ctx context.Context, settings *models.RoleResourceAccess) error
	GetAccessMap(ctx context.Context) (entities.AccessMap, error)
}
