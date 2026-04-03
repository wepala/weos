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

var defaultRoles = []string{"admin", "instructor"}

type RoleSettingsRepository struct {
	db *gorm.DB
}

func ProvideRoleSettingsRepository(db *gorm.DB) *RoleSettingsRepository {
	return &RoleSettingsRepository{db: db}
}

func (r *RoleSettingsRepository) Get(ctx context.Context) (*models.RoleSettings, error) {
	var settings models.RoleSettings
	result := r.db.WithContext(ctx).FirstOrCreate(&settings, models.RoleSettings{ID: 1})
	if result.Error != nil {
		return nil, result.Error
	}
	if settings.Roles == "" {
		raw, _ := json.Marshal(defaultRoles)
		settings.Roles = string(raw)
		_ = r.db.WithContext(ctx).Save(&settings).Error
	}
	return &settings, nil
}

func (r *RoleSettingsRepository) Save(ctx context.Context, settings *models.RoleSettings) error {
	settings.ID = 1
	return r.db.WithContext(ctx).Save(settings).Error
}

func (r *RoleSettingsRepository) GetRoleNames(ctx context.Context) ([]string, error) {
	settings, err := r.Get(ctx)
	if err != nil {
		return nil, err
	}
	var roles []string
	if err := json.Unmarshal([]byte(settings.Roles), &roles); err != nil {
		return defaultRoles, nil
	}
	return roles, nil
}
