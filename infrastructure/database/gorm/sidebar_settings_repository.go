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

	"weos/infrastructure/models"

	"gorm.io/gorm"
)

type SidebarSettingsRepository struct {
	db *gorm.DB
}

func ProvideSidebarSettingsRepository(db *gorm.DB) *SidebarSettingsRepository {
	return &SidebarSettingsRepository{db: db}
}

func (r *SidebarSettingsRepository) Get(ctx context.Context) (*models.SidebarSettings, error) {
	var settings models.SidebarSettings
	result := r.db.WithContext(ctx).FirstOrCreate(&settings, models.SidebarSettings{ID: 1})
	if result.Error != nil {
		return nil, result.Error
	}
	return &settings, nil
}

func (r *SidebarSettingsRepository) Save(ctx context.Context, settings *models.SidebarSettings) error {
	settings.ID = 1
	return r.db.WithContext(ctx).Save(settings).Error
}
