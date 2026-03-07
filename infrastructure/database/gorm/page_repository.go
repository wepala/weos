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

type PageRepository struct {
	db *gorm.DB
}

type PageRepositoryResult struct {
	fx.Out
	Repository repositories.PageRepository
}

func ProvidePageRepository(params struct {
	fx.In
	DB *gorm.DB
}) (PageRepositoryResult, error) {
	return PageRepositoryResult{
		Repository: &PageRepository{db: params.DB},
	}, nil
}

func (r *PageRepository) Save(
	ctx context.Context, entity *entities.Page,
) error {
	model := models.FromPage(entity)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save page: %w", err)
	}
	return nil
}

func (r *PageRepository) FindByID(
	ctx context.Context, id string,
) (*entities.Page, error) {
	var model models.Page
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find page: %w", err)
	}
	return model.ToPage()
}

func (r *PageRepository) FindAll(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Page], error) {
	return r.findPages(ctx, "", cursor, limit)
}

func (r *PageRepository) FindByWebsiteID(
	ctx context.Context, websiteID string, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Page], error) {
	return r.findPages(ctx, websiteID, cursor, limit)
}

func (r *PageRepository) findPages(
	ctx context.Context, websiteID, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Page], error) {
	if limit <= 0 {
		limit = 20
	}

	query := r.db.WithContext(ctx).Where("deleted_at IS NULL")
	if websiteID != "" {
		query = query.Where("website_id = ?", websiteID)
	}
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}

	var dbModels []models.Page
	err := query.Order("id ASC").Limit(limit + 1).Find(&dbModels).Error
	if err != nil {
		return repositories.PaginatedResponse[*entities.Page]{},
			fmt.Errorf("failed to list pages: %w", err)
	}

	return buildPagePage(dbModels, limit)
}

func buildPagePage(
	dbModels []models.Page, limit int,
) (repositories.PaginatedResponse[*entities.Page], error) {
	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.Page, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToPage()
		if err != nil {
			return repositories.PaginatedResponse[*entities.Page]{}, err
		}
		result = append(result, e)
		nextCursor = m.ID
	}

	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Page]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (r *PageRepository) Update(
	ctx context.Context, entity *entities.Page,
) error {
	model := models.FromPage(entity)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update page: %w", err)
	}
	return nil
}

func (r *PageRepository) Delete(ctx context.Context, id string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Model(&models.Page{}).
		Where("id = ?", id).Update("deleted_at", &now).Error
	if err != nil {
		return fmt.Errorf("failed to delete page: %w", err)
	}
	return nil
}
