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

type WebsiteRepository struct {
	db *gorm.DB
}

type WebsiteRepositoryResult struct {
	fx.Out
	Repository repositories.WebsiteRepository
}

func ProvideWebsiteRepository(params struct {
	fx.In
	DB *gorm.DB
}) (WebsiteRepositoryResult, error) {
	return WebsiteRepositoryResult{
		Repository: &WebsiteRepository{db: params.DB},
	}, nil
}

func (r *WebsiteRepository) Save(
	ctx context.Context, entity *entities.Website,
) error {
	model := models.FromWebsite(entity)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save website: %w", err)
	}
	return nil
}

func (r *WebsiteRepository) FindByID(
	ctx context.Context, id string,
) (*entities.Website, error) {
	var model models.Website
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find website: %w", err)
	}
	return model.ToWebsite()
}

func (r *WebsiteRepository) FindAll(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Website], error) {
	if limit <= 0 {
		limit = 20
	}

	query := r.db.WithContext(ctx).Where("deleted_at IS NULL")
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}

	var dbModels []models.Website
	if err := query.Order("id ASC").Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Website]{},
			fmt.Errorf("failed to list websites: %w", err)
	}

	return buildWebsitePage(dbModels, limit)
}

func buildWebsitePage(
	dbModels []models.Website, limit int,
) (repositories.PaginatedResponse[*entities.Website], error) {
	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.Website, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToWebsite()
		if err != nil {
			return repositories.PaginatedResponse[*entities.Website]{}, err
		}
		result = append(result, e)
		nextCursor = m.ID
	}

	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Website]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (r *WebsiteRepository) Update(
	ctx context.Context, entity *entities.Website,
) error {
	model := models.FromWebsite(entity)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update website: %w", err)
	}
	return nil
}

func (r *WebsiteRepository) Delete(ctx context.Context, id string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Model(&models.Website{}).
		Where("id = ?", id).Update("deleted_at", &now).Error
	if err != nil {
		return fmt.Errorf("failed to delete website: %w", err)
	}
	return nil
}
