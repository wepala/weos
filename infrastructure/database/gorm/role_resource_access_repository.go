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

package gorm

import (
	"context"
	"encoding/json"

	"weos/infrastructure/models"

	"gorm.io/gorm"
)

// AccessMap maps role → (resourceTypeSlug → []action).
// Actions are ODRL short names: "read", "modify", "delete".
type AccessMap map[string]map[string][]string

type RoleResourceAccessRepository struct {
	db *gorm.DB
}

func ProvideRoleResourceAccessRepository(db *gorm.DB) *RoleResourceAccessRepository {
	return &RoleResourceAccessRepository{db: db}
}

func (r *RoleResourceAccessRepository) Get(ctx context.Context) (*models.RoleResourceAccess, error) {
	var settings models.RoleResourceAccess
	result := r.db.WithContext(ctx).FirstOrCreate(&settings, models.RoleResourceAccess{ID: 1})
	if result.Error != nil {
		return nil, result.Error
	}
	if settings.Access == "" {
		settings.Access = "{}"
	}
	return &settings, nil
}

func (r *RoleResourceAccessRepository) Save(ctx context.Context, settings *models.RoleResourceAccess) error {
	settings.ID = 1
	return r.db.WithContext(ctx).Save(settings).Error
}

func (r *RoleResourceAccessRepository) GetAccessMap(ctx context.Context) (AccessMap, error) {
	settings, err := r.Get(ctx)
	if err != nil {
		return nil, err
	}
	var m AccessMap
	if err := json.Unmarshal([]byte(settings.Access), &m); err != nil {
		return AccessMap{}, nil
	}
	return m, nil
}
