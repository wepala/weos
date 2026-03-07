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

type PersonRepository struct {
	db *gorm.DB
}

type PersonRepositoryResult struct {
	fx.Out
	Repository repositories.PersonRepository
}

func ProvidePersonRepository(params struct {
	fx.In
	DB *gorm.DB
}) (PersonRepositoryResult, error) {
	return PersonRepositoryResult{
		Repository: &PersonRepository{db: params.DB},
	}, nil
}

func (r *PersonRepository) Save(
	ctx context.Context, entity *entities.Person,
) error {
	model := models.FromPerson(entity)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save person: %w", err)
	}
	return nil
}

func (r *PersonRepository) FindByID(
	ctx context.Context, id string,
) (*entities.Person, error) {
	var model models.Person
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find person: %w", err)
	}
	return model.ToPerson()
}

func (r *PersonRepository) FindAll(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Person], error) {
	if limit <= 0 {
		limit = 20
	}

	query := r.db.WithContext(ctx).Where("deleted_at IS NULL")
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}

	var dbModels []models.Person
	if err := query.Order("id ASC").Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Person]{},
			fmt.Errorf("failed to list persons: %w", err)
	}

	return buildPersonPage(dbModels, limit)
}

func buildPersonPage(
	dbModels []models.Person, limit int,
) (repositories.PaginatedResponse[*entities.Person], error) {
	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.Person, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToPerson()
		if err != nil {
			return repositories.PaginatedResponse[*entities.Person]{}, err
		}
		result = append(result, e)
		nextCursor = m.ID
	}

	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Person]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (r *PersonRepository) Update(
	ctx context.Context, entity *entities.Person,
) error {
	model := models.FromPerson(entity)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update person: %w", err)
	}
	return nil
}

func (r *PersonRepository) Delete(ctx context.Context, id string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Model(&models.Person{}).
		Where("id = ?", id).Update("deleted_at", &now).Error
	if err != nil {
		return fmt.Errorf("failed to delete person: %w", err)
	}
	return nil
}
