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
	"fmt"
	"time"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/infrastructure/models"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

type ThemeRepository struct {
	db *gorm.DB
}

type ThemeRepositoryResult struct {
	fx.Out
	Repository repositories.ThemeRepository
}

func ProvideThemeRepository(params struct {
	fx.In
	DB *gorm.DB
}) (ThemeRepositoryResult, error) {
	return ThemeRepositoryResult{
		Repository: &ThemeRepository{db: params.DB},
	}, nil
}

func (r *ThemeRepository) Save(
	ctx context.Context, entity *entities.Theme,
) error {
	model := models.FromTheme(entity)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save theme: %w", err)
	}
	return nil
}

func (r *ThemeRepository) FindByID(
	ctx context.Context, id string,
) (*entities.Theme, error) {
	var model models.Theme
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find theme: %w", err)
	}
	return model.ToTheme()
}

func (r *ThemeRepository) FindAll(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Theme], error) {
	if limit <= 0 {
		limit = 20
	}

	query := r.db.WithContext(ctx).Where("deleted_at IS NULL")
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}

	var dbModels []models.Theme
	if err := query.Order("id ASC").Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Theme]{},
			fmt.Errorf("failed to list themes: %w", err)
	}

	return buildThemePage(dbModels, limit)
}

func buildThemePage(
	dbModels []models.Theme, limit int,
) (repositories.PaginatedResponse[*entities.Theme], error) {
	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.Theme, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToTheme()
		if err != nil {
			return repositories.PaginatedResponse[*entities.Theme]{}, err
		}
		result = append(result, e)
		nextCursor = m.ID
	}

	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Theme]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (r *ThemeRepository) Update(
	ctx context.Context, entity *entities.Theme,
) error {
	model := models.FromTheme(entity)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update theme: %w", err)
	}
	return nil
}

func (r *ThemeRepository) Delete(ctx context.Context, id string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Model(&models.Theme{}).
		Where("id = ?", id).Update("deleted_at", &now).Error
	if err != nil {
		return fmt.Errorf("failed to delete theme: %w", err)
	}
	return nil
}
