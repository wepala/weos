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

type SectionRepository struct {
	db *gorm.DB
}

type SectionRepositoryResult struct {
	fx.Out
	Repository repositories.SectionRepository
}

func ProvideSectionRepository(params struct {
	fx.In
	DB *gorm.DB
}) (SectionRepositoryResult, error) {
	return SectionRepositoryResult{
		Repository: &SectionRepository{db: params.DB},
	}, nil
}

func (r *SectionRepository) Save(
	ctx context.Context, entity *entities.Section, pageID string,
) error {
	model := models.FromSection(entity, pageID)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save section: %w", err)
	}
	return nil
}

func (r *SectionRepository) FindByID(
	ctx context.Context, id string,
) (*entities.Section, error) {
	var model models.Section
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find section: %w", err)
	}
	return model.ToSection()
}

func (r *SectionRepository) FindAll(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Section], error) {
	return r.findSections(ctx, "", cursor, limit)
}

func (r *SectionRepository) FindByPageID(
	ctx context.Context, pageID string, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Section], error) {
	return r.findSections(ctx, pageID, cursor, limit)
}

func (r *SectionRepository) findSections(
	ctx context.Context, pageID, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Section], error) {
	if limit <= 0 {
		limit = 20
	}

	query := r.db.WithContext(ctx).Where("deleted_at IS NULL")
	if pageID != "" {
		query = query.Where("page_id = ?", pageID)
	}
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}

	var dbModels []models.Section
	err := query.Order("id ASC").Limit(limit + 1).Find(&dbModels).Error
	if err != nil {
		return repositories.PaginatedResponse[*entities.Section]{},
			fmt.Errorf("failed to list sections: %w", err)
	}

	return buildSectionPage(dbModels, limit)
}

func buildSectionPage(
	dbModels []models.Section, limit int,
) (repositories.PaginatedResponse[*entities.Section], error) {
	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.Section, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToSection()
		if err != nil {
			return repositories.PaginatedResponse[*entities.Section]{}, err
		}
		result = append(result, e)
		nextCursor = m.ID
	}

	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Section]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (r *SectionRepository) Update(
	ctx context.Context, entity *entities.Section, pageID string,
) error {
	model := models.FromSection(entity, pageID)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update section: %w", err)
	}
	return nil
}

func (r *SectionRepository) Delete(ctx context.Context, id string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Model(&models.Section{}).
		Where("id = ?", id).Update("deleted_at", &now).Error
	if err != nil {
		return fmt.Errorf("failed to delete section: %w", err)
	}
	return nil
}
