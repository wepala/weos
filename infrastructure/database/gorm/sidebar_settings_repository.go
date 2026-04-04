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
	"errors"

	"weos/infrastructure/models"

	"gorm.io/gorm"
)

const defaultRole = "default"

type SidebarSettingsRepository struct {
	db *gorm.DB
}

func ProvideSidebarSettingsRepository(db *gorm.DB) *SidebarSettingsRepository {
	return &SidebarSettingsRepository{db: db}
}

// Get returns the default sidebar settings (backward compat).
func (r *SidebarSettingsRepository) Get(ctx context.Context) (*models.SidebarSettings, error) {
	return r.GetByRole(ctx, defaultRole)
}

// Save saves the default sidebar settings (backward compat).
func (r *SidebarSettingsRepository) Save(ctx context.Context, settings *models.SidebarSettings) error {
	return r.SaveByRole(ctx, defaultRole, settings)
}

// GetByRole returns sidebar settings for the given role.
// Falls back to "default" if no role-specific settings exist.
func (r *SidebarSettingsRepository) GetByRole(ctx context.Context, role string) (*models.SidebarSettings, error) {
	if role == "" {
		role = defaultRole
	}

	var settings models.SidebarSettings
	err := r.db.WithContext(ctx).Where("role = ?", role).First(&settings).Error
	if err == nil {
		return &settings, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Role-specific settings not found — fall back to default.
	if role != defaultRole {
		err = r.db.WithContext(ctx).Where("role = ?", defaultRole).First(&settings).Error
		if err == nil {
			return &settings, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	// No settings at all — create the default row.
	settings = models.SidebarSettings{Role: defaultRole}
	if createErr := r.db.WithContext(ctx).Create(&settings).Error; createErr != nil {
		return nil, createErr
	}
	return &settings, nil
}

// SaveByRole saves sidebar settings for the given role (upsert).
func (r *SidebarSettingsRepository) SaveByRole(ctx context.Context, role string, settings *models.SidebarSettings) error {
	if role == "" {
		role = defaultRole
	}

	var existing models.SidebarSettings
	err := r.db.WithContext(ctx).Where("role = ?", role).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		settings.ID = 0
		settings.Role = role
		return r.db.WithContext(ctx).Create(settings).Error
	}

	settings.ID = existing.ID
	settings.Role = role
	return r.db.WithContext(ctx).Save(settings).Error
}
