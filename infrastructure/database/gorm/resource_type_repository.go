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
	"fmt"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

type ResourceTypeRepository struct {
	db *gorm.DB
}

type ResourceTypeRepositoryResult struct {
	fx.Out
	Repository repositories.ResourceTypeRepository
}

func ProvideResourceTypeRepository(params struct {
	fx.In
	DB *gorm.DB
}) (ResourceTypeRepositoryResult, error) {
	return ResourceTypeRepositoryResult{
		Repository: &ResourceTypeRepository{db: params.DB},
	}, nil
}

func (r *ResourceTypeRepository) Save(
	ctx context.Context, entity *entities.ResourceType,
) error {
	model := models.FromResourceType(entity)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save resource type: %w", err)
	}
	return nil
}

func (r *ResourceTypeRepository) FindByID(
	ctx context.Context, id string,
) (*entities.ResourceType, error) {
	var model models.ResourceType
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find resource type: %w", err)
	}
	return model.ToResourceType()
}

func (r *ResourceTypeRepository) FindBySlug(
	ctx context.Context, slug string,
) (*entities.ResourceType, error) {
	var model models.ResourceType
	err := r.db.WithContext(ctx).
		Where("slug = ? AND deleted_at IS NULL", slug).First(&model).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("resource type %q: %w", slug, repositories.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to find resource type by slug: %w", err)
	}
	return model.ToResourceType()
}

func (r *ResourceTypeRepository) FindAll(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	if limit <= 0 {
		limit = 20
	}

	query := r.db.WithContext(ctx).Where("deleted_at IS NULL")
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}

	var dbModels []models.ResourceType
	if err := query.Order("id ASC").Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.ResourceType]{},
			fmt.Errorf("failed to list resource types: %w", err)
	}

	return buildResourceTypePage(dbModels, limit)
}

func buildResourceTypePage(
	dbModels []models.ResourceType, limit int,
) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.ResourceType, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToResourceType()
		if err != nil {
			return repositories.PaginatedResponse[*entities.ResourceType]{}, err
		}
		result = append(result, e)
		nextCursor = m.ID
	}

	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.ResourceType]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (r *ResourceTypeRepository) Update(
	ctx context.Context, entity *entities.ResourceType,
) error {
	model := models.FromResourceType(entity)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update resource type: %w", err)
	}
	return nil
}

func (r *ResourceTypeRepository) Delete(ctx context.Context, id string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Model(&models.ResourceType{}).
		Where("id = ?", id).Update("deleted_at", &now).Error
	if err != nil {
		return fmt.Errorf("failed to delete resource type: %w", err)
	}
	return nil
}
