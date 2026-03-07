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

type OrganizationRepository struct {
	db *gorm.DB
}

type OrganizationRepositoryResult struct {
	fx.Out
	Repository repositories.OrganizationRepository
}

func ProvideOrganizationRepository(params struct {
	fx.In
	DB *gorm.DB
}) (OrganizationRepositoryResult, error) {
	return OrganizationRepositoryResult{
		Repository: &OrganizationRepository{db: params.DB},
	}, nil
}

func (r *OrganizationRepository) Save(
	ctx context.Context, entity *entities.Organization,
) error {
	model := models.FromOrganization(entity)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save organization: %w", err)
	}
	return nil
}

func (r *OrganizationRepository) FindByID(
	ctx context.Context, id string,
) (*entities.Organization, error) {
	var model models.Organization
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find organization: %w", err)
	}
	return model.ToOrganization()
}

func (r *OrganizationRepository) FindAll(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Organization], error) {
	if limit <= 0 {
		limit = 20
	}

	query := r.db.WithContext(ctx).Where("deleted_at IS NULL")
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}

	var dbModels []models.Organization
	if err := query.Order("id ASC").Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Organization]{},
			fmt.Errorf("failed to list organizations: %w", err)
	}

	return buildOrganizationPage(dbModels, limit)
}

func buildOrganizationPage(
	dbModels []models.Organization, limit int,
) (repositories.PaginatedResponse[*entities.Organization], error) {
	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.Organization, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToOrganization()
		if err != nil {
			return repositories.PaginatedResponse[*entities.Organization]{}, err
		}
		result = append(result, e)
		nextCursor = m.ID
	}

	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Organization]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (r *OrganizationRepository) Update(
	ctx context.Context, entity *entities.Organization,
) error {
	model := models.FromOrganization(entity)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update organization: %w", err)
	}
	return nil
}

func (r *OrganizationRepository) Delete(ctx context.Context, id string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Model(&models.Organization{}).
		Where("id = ?", id).Update("deleted_at", &now).Error
	if err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}
	return nil
}
