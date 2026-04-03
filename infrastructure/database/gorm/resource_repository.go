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
	"encoding/base64"
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

// genericSortableColumns are allowed for sorting in the generic resources table.
var genericSortableColumns = map[string]bool{
	"id":         true,
	"created_at": true,
	"status":     true,
}

type cursorData struct {
	Value string `json:"v"`
	ID    string `json:"id"`
}

func encodeCursor(sortValue, id string) string {
	data, _ := json.Marshal(cursorData{Value: sortValue, ID: id})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(cursor string) (cursorData, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return cursorData{}, err
	}
	var cd cursorData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return cursorData{}, err
	}
	return cd, nil
}

func normalizeSortOptions(sort repositories.SortOptions) (string, string) {
	sortBy := sort.SortBy
	if sortBy == "" {
		sortBy = "id"
	}
	sortOrder := sort.SortOrder
	if sortOrder != "desc" {
		sortOrder = "asc"
	}
	return sortBy, sortOrder
}

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
	DB            *gorm.DB
	ProjectionMgr repositories.ProjectionManager
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
	ctx context.Context, typeSlug string, cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	if limit <= 0 {
		limit = 20
	}

	if r.projMgr.HasProjectionTable(typeSlug) {
		return r.findAllFromProjection(ctx, typeSlug, cursor, limit, sort)
	}

	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := camelToSnake(sortBy)
	if !genericSortableColumns[colName] {
		colName = "id"
	}

	query := r.db.WithContext(ctx).
		Where("type_slug = ? AND deleted_at IS NULL", typeSlug)
	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, colName, sortOrder, cd)
		}
	}

	orderClause := fmt.Sprintf("%s %s, id %s", colName, sortOrder, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("id %s", sortOrder)
	}

	var dbModels []models.Resource
	if err := query.Order(orderClause).Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list resources: %w", err)
	}

	return buildResourcePageWithCursor(
		dbModels, limit, colName, sortOrder, func(m models.Resource) string {
			if colName == "id" {
				return m.ID
			}
			if colName == "created_at" {
				return m.CreatedAt.Format(time.RFC3339Nano)
			}
			return m.Status
		})
}

func (r *ResourceRepository) findAllFromProjection(
	ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	tableName := r.projMgr.TableName(typeSlug)
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := camelToSnake(sortBy)

	// Validate that the column exists; fall back to id.
	if colName != "id" && !standardColumnNames[colName] {
		if !r.db.Migrator().HasColumn(tableName, colName) {
			colName = "id"
		}
	}

	selectCols := "id, type_slug, data, status, sequence_no, created_at"
	// Include the sort column if it's not already in the standard select.
	if !standardColumnNames[colName] && colName != "id" {
		selectCols += ", " + colName
	}

	query := r.db.WithContext(ctx).Table(tableName).
		Select(selectCols).
		Where("deleted_at IS NULL")
	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, colName, sortOrder, cd)
		}
	}

	orderClause := fmt.Sprintf("%s %s, id %s", colName, sortOrder, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("id %s", sortOrder)
	}

	var rows []map[string]any
	if err := query.Order(orderClause).Limit(limit + 1).Find(&rows).Error; err != nil {
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
		id := fmt.Sprint(row["id"])
		e := &entities.Resource{}
		if err := e.Restore(
			id,
			fmt.Sprint(row["type_slug"]),
			fmt.Sprint(row["status"]),
			json.RawMessage(fmt.Sprint(row["data"])),
			parseTime(row["created_at"]),
			toInt(row["sequence_no"]),
		); err != nil {
			return repositories.PaginatedResponse[*entities.Resource]{}, err
		}
		result = append(result, e)

		// Build cursor from the sort column value.
		sortVal := row[colName]
		if sortVal == nil {
			sortVal = row["id"]
		}
		nextCursor = encodeCursor(fmt.Sprint(sortVal), id)
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

func applyCursorCondition(
	query *gorm.DB, colName, sortOrder string, cd cursorData,
) *gorm.DB {
	if colName == "id" {
		if sortOrder == "desc" {
			return query.Where("id < ?", cd.ID)
		}
		return query.Where("id > ?", cd.ID)
	}
	if sortOrder == "desc" {
		return query.Where(
			fmt.Sprintf("(%s < ?) OR (%s = ? AND id < ?)", colName, colName),
			cd.Value, cd.Value, cd.ID,
		)
	}
	return query.Where(
		fmt.Sprintf("(%s > ?) OR (%s = ? AND id > ?)", colName, colName),
		cd.Value, cd.Value, cd.ID,
	)
}

func buildResourcePageWithCursor(
	dbModels []models.Resource, limit int, colName, sortOrder string,
	sortValFn func(models.Resource) string,
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
		nextCursor = encodeCursor(sortValFn(m), m.ID)
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

func parseTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		parsed, _ := time.Parse(time.RFC3339Nano, t)
		return parsed
	default:
		return time.Time{}
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// filterOperators maps API operator names to SQL operators.
var filterOperators = map[string]string{
	"eq":  "=",
	"ne":  "!=",
	"gt":  ">",
	"gte": ">=",
	"lt":  "<",
	"lte": "<=",
}

func (r *ResourceRepository) FindAllByTypeWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	if limit <= 0 {
		limit = 20
	}

	if r.projMgr.HasProjectionTable(typeSlug) {
		return r.findAllFromProjectionWithFilters(ctx, typeSlug, filters, cursor, limit, sort)
	}
	return r.findAllFromGenericWithFilters(ctx, typeSlug, filters, cursor, limit, sort)
}

func (r *ResourceRepository) findAllFromGenericWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := camelToSnake(sortBy)
	if !genericSortableColumns[colName] {
		colName = "id"
	}

	query := r.db.WithContext(ctx).
		Where("type_slug = ? AND deleted_at IS NULL", typeSlug)

	// Apply filters using json_extract for the generic table.
	for _, f := range filters {
		sqlOp, ok := filterOperators[f.Operator]
		if !ok {
			continue
		}
		query = query.Where(
			fmt.Sprintf("json_extract(data, '$.%s') %s ?", f.Field, sqlOp),
			f.Value,
		)
	}

	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, colName, sortOrder, cd)
		}
	}

	orderClause := fmt.Sprintf("%s %s, id %s", colName, sortOrder, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("id %s", sortOrder)
	}

	var dbModels []models.Resource
	if err := query.Order(orderClause).Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list resources with filters: %w", err)
	}

	return buildResourcePageWithCursor(
		dbModels, limit, colName, sortOrder, func(m models.Resource) string {
			if colName == "id" {
				return m.ID
			}
			if colName == "created_at" {
				return m.CreatedAt.Format(time.RFC3339Nano)
			}
			return m.Status
		})
}

func (r *ResourceRepository) findAllFromProjectionWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	tableName := r.projMgr.TableName(typeSlug)
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := camelToSnake(sortBy)

	if colName != "id" && !standardColumnNames[colName] {
		if !r.db.Migrator().HasColumn(tableName, colName) {
			colName = "id"
		}
	}

	selectCols := "id, type_slug, data, status, sequence_no, created_at"
	if !standardColumnNames[colName] && colName != "id" {
		selectCols += ", " + colName
	}

	query := r.db.WithContext(ctx).Table(tableName).
		Select(selectCols).
		Where("deleted_at IS NULL")

	// Apply filters on projection columns.
	for _, f := range filters {
		sqlOp, ok := filterOperators[f.Operator]
		if !ok {
			continue
		}
		filterCol := camelToSnake(f.Field)
		// Silently skip non-existent columns.
		if !standardColumnNames[filterCol] && filterCol != "id" {
			if !r.db.Migrator().HasColumn(tableName, filterCol) {
				continue
			}
		}
		query = query.Where(fmt.Sprintf("%s %s ?", filterCol, sqlOp), f.Value)

		// Include filter column in SELECT if needed for reading.
		if !standardColumnNames[filterCol] && filterCol != "id" && filterCol != colName {
			selectCols += ", " + filterCol
		}
	}

	// Re-apply select in case filter columns were added.
	query = query.Select(selectCols)

	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, colName, sortOrder, cd)
		}
	}

	orderClause := fmt.Sprintf("%s %s, id %s", colName, sortOrder, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("id %s", sortOrder)
	}

	var rows []map[string]any
	if err := query.Order(orderClause).Limit(limit + 1).Find(&rows).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list resources from %s with filters: %w", tableName, err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	result := make([]*entities.Resource, 0, len(rows))
	var nextCursor string
	for _, row := range rows {
		id := fmt.Sprint(row["id"])
		e := &entities.Resource{}
		if err := e.Restore(
			id,
			fmt.Sprint(row["type_slug"]),
			fmt.Sprint(row["status"]),
			json.RawMessage(fmt.Sprint(row["data"])),
			parseTime(row["created_at"]),
			toInt(row["sequence_no"]),
		); err != nil {
			return repositories.PaginatedResponse[*entities.Resource]{}, err
		}
		result = append(result, e)

		sortVal := row[colName]
		if sortVal == nil {
			sortVal = row["id"]
		}
		nextCursor = encodeCursor(fmt.Sprint(sortVal), id)
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
