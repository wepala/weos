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
	"encoding/json"
	"fmt"
	"time"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/infrastructure/models"
	"weos/pkg/identity"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

type ResourceRepository struct {
	db      *gorm.DB
	projMgr repositories.ProjectionManager
}

type ResourceRepositoryResult struct {
	fx.Out
	Repository repositories.ResourceRepository
}

func ProvideResourceRepository(params struct {
	fx.In
	DB              *gorm.DB
	ProjectionMgr   repositories.ProjectionManager
}) (ResourceRepositoryResult, error) {
	return ResourceRepositoryResult{
		Repository: &ResourceRepository{
			db:      params.DB,
			projMgr: params.ProjectionMgr,
		},
	}, nil
}

func (r *ResourceRepository) Save(
	ctx context.Context, entity *entities.Resource,
) error {
	if r.projMgr.HasProjectionTable(entity.TypeSlug()) {
		return r.saveToProjection(ctx, entity)
	}
	model := models.FromResource(entity)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save resource: %w", err)
	}
	return nil
}

func (r *ResourceRepository) saveToProjection(
	ctx context.Context, entity *entities.Resource,
) error {
	tableName := r.projMgr.TableName(entity.TypeSlug())
	row := map[string]any{
		"id":          entity.GetID(),
		"type_slug":   entity.TypeSlug(),
		"data":        string(entity.Data()),
		"status":      entity.Status(),
		"sequence_no": entity.GetSequenceNo(),
		"created_at":  entity.CreatedAt(),
	}
	ExtractFlatColumns(entity.Data(), row)
	if err := r.db.WithContext(ctx).Table(tableName).Create(row).Error; err != nil {
		return fmt.Errorf("failed to save resource to projection %s: %w", tableName, err)
	}
	return nil
}

func (r *ResourceRepository) FindByID(
	ctx context.Context, id string,
) (*entities.Resource, error) {
	typeSlug := identity.ExtractResourceTypeSlug(id)
	if typeSlug != "" && r.projMgr.HasProjectionTable(typeSlug) {
		return r.findByIDFromProjection(ctx, id, typeSlug)
	}
	var model models.Resource
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find resource: %w", err)
	}
	return model.ToResource()
}

func (r *ResourceRepository) findByIDFromProjection(
	ctx context.Context, id, typeSlug string,
) (*entities.Resource, error) {
	tableName := r.projMgr.TableName(typeSlug)
	var result struct {
		ID         string
		TypeSlug   string
		Data       string
		Status     string
		SequenceNo int
		CreatedAt  time.Time
	}
	err := r.db.WithContext(ctx).Table(tableName).
		Select("id, type_slug, data, status, sequence_no, created_at").
		Where("id = ? AND deleted_at IS NULL", id).
		Take(&result).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find resource in %s: %w", tableName, err)
	}
	e := &entities.Resource{}
	if err := e.Restore(
		result.ID, result.TypeSlug, result.Status,
		json.RawMessage(result.Data),
		result.CreatedAt, result.SequenceNo,
	); err != nil {
		return nil, err
	}
	return e, nil
}

func (r *ResourceRepository) FindAllByType(
	ctx context.Context, typeSlug string, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	if limit <= 0 {
		limit = 20
	}

	if r.projMgr.HasProjectionTable(typeSlug) {
		return r.findAllFromProjection(ctx, typeSlug, cursor, limit)
	}

	query := r.db.WithContext(ctx).
		Where("type_slug = ? AND deleted_at IS NULL", typeSlug)
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}

	var dbModels []models.Resource
	if err := query.Order("id ASC").Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list resources: %w", err)
	}

	return buildResourcePage(dbModels, limit)
}

func (r *ResourceRepository) findAllFromProjection(
	ctx context.Context, typeSlug, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	tableName := r.projMgr.TableName(typeSlug)
	query := r.db.WithContext(ctx).Table(tableName).
		Select("id, type_slug, data, status, sequence_no, created_at").
		Where("deleted_at IS NULL")
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}

	var rows []struct {
		ID         string
		TypeSlug   string
		Data       string
		Status     string
		SequenceNo int
		CreatedAt  time.Time
	}
	if err := query.Order("id ASC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list resources from %s: %w", tableName, err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	result := make([]*entities.Resource, 0, len(rows))
	var nextCursor string
	for _, row := range rows {
		e := &entities.Resource{}
		if err := e.Restore(
			row.ID, row.TypeSlug, row.Status,
			json.RawMessage(row.Data),
			row.CreatedAt, row.SequenceNo,
		); err != nil {
			return repositories.PaginatedResponse[*entities.Resource]{}, err
		}
		result = append(result, e)
		nextCursor = row.ID
	}
	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Resource]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func buildResourcePage(
	dbModels []models.Resource, limit int,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.Resource, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToResource()
		if err != nil {
			return repositories.PaginatedResponse[*entities.Resource]{}, err
		}
		result = append(result, e)
		nextCursor = m.ID
	}

	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Resource]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (r *ResourceRepository) Update(
	ctx context.Context, entity *entities.Resource,
) error {
	if r.projMgr.HasProjectionTable(entity.TypeSlug()) {
		return r.updateProjection(ctx, entity)
	}
	model := models.FromResource(entity)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update resource: %w", err)
	}
	return nil
}

func (r *ResourceRepository) updateProjection(
	ctx context.Context, entity *entities.Resource,
) error {
	tableName := r.projMgr.TableName(entity.TypeSlug())
	row := map[string]any{
		"data":        string(entity.Data()),
		"status":      entity.Status(),
		"sequence_no": entity.GetSequenceNo(),
		"updated_at":  time.Now(),
	}
	ExtractFlatColumns(entity.Data(), row)
	err := r.db.WithContext(ctx).Table(tableName).
		Where("id = ?", entity.GetID()).Updates(row).Error
	if err != nil {
		return fmt.Errorf("failed to update resource in %s: %w", tableName, err)
	}
	return nil
}

func (r *ResourceRepository) Delete(ctx context.Context, id string) error {
	typeSlug := identity.ExtractResourceTypeSlug(id)
	if typeSlug != "" && r.projMgr.HasProjectionTable(typeSlug) {
		return r.deleteFromProjection(ctx, id, typeSlug)
	}
	now := time.Now()
	err := r.db.WithContext(ctx).Model(&models.Resource{}).
		Where("id = ?", id).Update("deleted_at", &now).Error
	if err != nil {
		return fmt.Errorf("failed to delete resource: %w", err)
	}
	return nil
}

func (r *ResourceRepository) deleteFromProjection(
	ctx context.Context, id, typeSlug string,
) error {
	tableName := r.projMgr.TableName(typeSlug)
	now := time.Now()
	err := r.db.WithContext(ctx).Table(tableName).
		Where("id = ?", id).Update("deleted_at", &now).Error
	if err != nil {
		return fmt.Errorf("failed to delete resource from %s: %w", tableName, err)
	}
	return nil
}
