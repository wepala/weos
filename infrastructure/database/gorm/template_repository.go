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

type TemplateRepository struct {
	db *gorm.DB
}

type TemplateRepositoryResult struct {
	fx.Out
	Repository repositories.TemplateRepository
}

func ProvideTemplateRepository(params struct {
	fx.In
	DB *gorm.DB
}) (TemplateRepositoryResult, error) {
	return TemplateRepositoryResult{
		Repository: &TemplateRepository{db: params.DB},
	}, nil
}

func (r *TemplateRepository) Save(
	ctx context.Context, entity *entities.Template, themeID string,
) error {
	model := models.FromTemplate(entity, themeID)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save template: %w", err)
	}
	return nil
}

func (r *TemplateRepository) FindByID(
	ctx context.Context, id string,
) (*entities.Template, error) {
	var model models.Template
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find template: %w", err)
	}
	return model.ToTemplate()
}

func (r *TemplateRepository) FindAll(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Template], error) {
	return r.findTemplates(ctx, "", cursor, limit)
}

func (r *TemplateRepository) FindByThemeID(
	ctx context.Context, themeID string, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Template], error) {
	return r.findTemplates(ctx, themeID, cursor, limit)
}

func (r *TemplateRepository) findTemplates(
	ctx context.Context, themeID, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Template], error) {
	if limit <= 0 {
		limit = 20
	}

	query := r.db.WithContext(ctx).Where("deleted_at IS NULL")
	if themeID != "" {
		query = query.Where("theme_id = ?", themeID)
	}
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}

	var dbModels []models.Template
	err := query.Order("id ASC").Limit(limit + 1).Find(&dbModels).Error
	if err != nil {
		return repositories.PaginatedResponse[*entities.Template]{},
			fmt.Errorf("failed to list templates: %w", err)
	}

	return buildTemplatePage(dbModels, limit)
}

func buildTemplatePage(
	dbModels []models.Template, limit int,
) (repositories.PaginatedResponse[*entities.Template], error) {
	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.Template, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToTemplate()
		if err != nil {
			return repositories.PaginatedResponse[*entities.Template]{}, err
		}
		result = append(result, e)
		nextCursor = m.ID
	}

	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Template]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (r *TemplateRepository) Update(
	ctx context.Context, entity *entities.Template, themeID string,
) error {
	model := models.FromTemplate(entity, themeID)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update template: %w", err)
	}
	return nil
}

func (r *TemplateRepository) Delete(ctx context.Context, id string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Model(&models.Template{}).
		Where("id = ?", id).Update("deleted_at", &now).Error
	if err != nil {
		return fmt.Errorf("failed to delete template: %w", err)
	}
	return nil
}
