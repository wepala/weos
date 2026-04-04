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
	"weos/pkg/utils"

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
	data, err := json.Marshal(cursorData{Value: sortValue, ID: id})
	if err != nil {
		return ""
	}
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
	// Always save to the generic resources table (canonical store).
	model := models.FromResource(entity)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save resource: %w", err)
	}
	// Also save to the projection table (denormalized read model) if one exists.
	if r.projMgr.HasProjectionTable(entity.TypeSlug()) {
		if err := r.saveToProjection(ctx, entity); err != nil {
			return err
		}
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
		"status":      entity.Status(),
		"created_by":  entity.CreatedBy(),
		"account_id":  entity.AccountID(),
		"sequence_no": entity.GetSequenceNo(),
		"created_at":  entity.CreatedAt(),
	}
	ldCtx := r.projMgr.Context(entity.TypeSlug())
	ExtractFlatColumns(entity.Data(), ldCtx, row)
	if err := r.db.WithContext(ctx).Table(tableName).Create(row).Error; err != nil {
		return fmt.Errorf("failed to save resource to projection %s: %w", tableName, err)
	}
	return nil
}

func (r *ResourceRepository) FindByID(
	ctx context.Context, id string,
) (*entities.Resource, error) {
	// Always read from the canonical resources table (has the JSON-LD data).
	var model models.Resource
	err := r.db.WithContext(ctx).
		Where("id = ? ", id).First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find resource: %w", err)
	}
	return model.ToResource()
}

// applyVisibilityScope adds ownership filtering to a query when a non-nil scope
// is provided and the caller is not an admin.
func applyVisibilityScope(query *gorm.DB, scope *repositories.VisibilityScope, tablePrefix string) *gorm.DB {
	if scope == nil || scope.IsAdmin {
		return query
	}
	col := "created_by"
	if tablePrefix != "" {
		col = tablePrefix + "." + col
	}
	return query.Where(
		col+" = ? OR id IN (SELECT resource_id FROM resource_permissions WHERE agent_id = ? AND actions LIKE ?)",
		scope.AgentID, scope.AgentID, `%"read"%`,
	)
}

func (r *ResourceRepository) FindAllByType(
	ctx context.Context, typeSlug string, cursor string, limit int,
	sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	if limit <= 0 {
		limit = 20
	}

	if r.projMgr.HasProjectionTable(typeSlug) {
		return r.findAllFromProjection(ctx, typeSlug, cursor, limit, sort, scope)
	}

	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := utils.CamelToSnake(sortBy)
	if !genericSortableColumns[colName] {
		colName = "id"
	}

	query := r.db.WithContext(ctx).
		Where("type_slug = ? ", typeSlug)
	query = applyVisibilityScope(query, scope, "")
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

	return buildResourcePageWithCursor(dbModels, limit, colName, sortOrder, func(m models.Resource) string {
		if colName == "id" {
			return m.ID
		}
		if colName == "created_at" {
			return m.CreatedAt.Format(time.RFC3339Nano)
		}
		return m.Status
	})
}

//nolint:dupl // raw SQL row processing differs in sort/filter logic
func (r *ResourceRepository) findAllFromProjection(
	ctx context.Context, typeSlug, cursor string, limit int,
	sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	tableName := r.projMgr.TableName(typeSlug)
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := utils.CamelToSnake(sortBy)

	// Validate that the column exists; fall back to id.
	if colName != "id" && !standardColumnNames[colName] {
		if !r.db.Migrator().HasColumn(tableName, colName) {
			colName = "id"
		}
	}

	// Join with the resources table to get the JSON-LD data (not stored in projection).
	tbl := tableName
	selectCols := fmt.Sprintf("%s.id, %s.type_slug, resources.data, %s.status, %s.sequence_no, %s.created_at",
		tbl, tbl, tbl, tbl, tbl)
	if !standardColumnNames[colName] && colName != "id" {
		selectCols += fmt.Sprintf(", %s.%s", tbl, colName)
	}

	qualifiedCol := tbl + "." + colName
	query := r.db.WithContext(ctx).Table(tableName).
		Select(selectCols).
		Joins(fmt.Sprintf("JOIN resources ON %s.id = resources.id", tbl)).
		Where("1=1")
	query = applyVisibilityScope(query, scope, tbl)
	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, qualifiedCol, sortOrder, cd)
		}
	}

	orderClause := fmt.Sprintf("%s %s, %s.id %s", qualifiedCol, sortOrder, tbl, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("%s.id %s", tbl, sortOrder)
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
			toString(row["created_by"]), toString(row["account_id"]),
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

func applyCursorCondition(query *gorm.DB, colName, sortOrder string, cd cursorData) *gorm.DB {
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

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func parseTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, t)
		if err != nil {
			return time.Time{}
		}
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

func (r *ResourceRepository) FindAllByTypeAndField(
	ctx context.Context, typeSlug, fieldName, fieldValue string,
) ([]*entities.Resource, error) {
	if r.projMgr.HasProjectionTable(typeSlug) {
		return r.findAllByFieldFromProjection(ctx, typeSlug, fieldName, fieldValue)
	}
	return r.findAllByFieldFromGeneric(ctx, typeSlug, fieldName, fieldValue)
}

func (r *ResourceRepository) findAllByFieldFromProjection(
	ctx context.Context, typeSlug, fieldName, fieldValue string,
) ([]*entities.Resource, error) {
	tableName := r.projMgr.TableName(typeSlug)
	colName := utils.CamelToSnake(fieldName)
	if colName != "id" && !standardColumnNames[colName] {
		if !r.db.Migrator().HasColumn(tableName, colName) {
			return nil, fmt.Errorf("invalid field: %s", fieldName)
		}
	}
	tbl := tableName

	var rows []struct {
		ID         string
		TypeSlug   string
		Data       string
		Status     string
		CreatedBy  string
		AccountID  string
		SequenceNo int
		CreatedAt  time.Time
	}
	err := r.db.WithContext(ctx).Table(tableName).
		Select(fmt.Sprintf("%s.id, %s.type_slug, resources.data, %s.status, resources.created_by, resources.account_id, %s.sequence_no, %s.created_at",
			tbl, tbl, tbl, tbl, tbl)).
		Joins(fmt.Sprintf("JOIN resources ON %s.id = resources.id", tbl)).
		Where(tbl+"."+colName+" = ? ", fieldValue).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query %s by %s: %w", tableName, colName, err)
	}

	result := make([]*entities.Resource, 0, len(rows))
	for _, row := range rows {
		e := &entities.Resource{}
		if err := e.Restore(
			row.ID, row.TypeSlug, row.Status,
			json.RawMessage(row.Data),
			row.CreatedBy, row.AccountID,
			row.CreatedAt, row.SequenceNo,
		); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

func (r *ResourceRepository) findAllByFieldFromGeneric(
	ctx context.Context, typeSlug, fieldName, fieldValue string,
) ([]*entities.Resource, error) {
	var dbModels []models.Resource
	err := r.db.WithContext(ctx).
		Where("type_slug = ? ", typeSlug).
		Where("json_extract(data, ?) = ?", "$."+fieldName, fieldValue).
		Find(&dbModels).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query resources by %s: %w", fieldName, err)
	}

	result := make([]*entities.Resource, 0, len(dbModels))
	for _, m := range dbModels {
		e, err := m.ToResource()
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

var operatorMap = map[string]string{
	"eq": "=", "ne": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=",
}

func (r *ResourceRepository) FindAllByTypeWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	if r.projMgr.HasProjectionTable(typeSlug) {
		return r.findAllFromProjectionWithFilters(ctx, typeSlug, filters, cursor, limit, sort, scope)
	}
	return r.findAllFromGenericWithFilters(ctx, typeSlug, filters, cursor, limit, sort, scope)
}

//nolint:dupl // raw SQL row processing differs in sort/filter logic
func (r *ResourceRepository) findAllFromProjectionWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	tableName := r.projMgr.TableName(typeSlug)
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := utils.CamelToSnake(sortBy)

	if colName != "id" && !standardColumnNames[colName] {
		if !r.db.Migrator().HasColumn(tableName, colName) {
			colName = "id"
		}
	}

	tbl := tableName
	selectCols := fmt.Sprintf("%s.id, %s.type_slug, resources.data, %s.status, %s.sequence_no, %s.created_at",
		tbl, tbl, tbl, tbl, tbl)
	if !standardColumnNames[colName] && colName != "id" {
		selectCols += fmt.Sprintf(", %s.%s", tbl, colName)
	}

	qualifiedCol := tbl + "." + colName
	query := r.db.WithContext(ctx).Table(tableName).Select(selectCols).
		Joins(fmt.Sprintf("JOIN resources ON %s.id = resources.id", tbl)).
		Where("1=1")
	query = applyVisibilityScope(query, scope, tbl)

	for _, f := range filters {
		sqlOp, ok := operatorMap[f.Operator]
		if !ok {
			continue
		}
		fc := utils.CamelToSnake(f.Field)
		if !standardColumnNames[fc] && fc != "id" {
			if !r.db.Migrator().HasColumn(tableName, fc) {
				continue
			}
		}
		query = query.Where(tbl+"."+fc+" "+sqlOp+" ?", f.Value)
	}

	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, qualifiedCol, sortOrder, cd)
		}
	}

	orderClause := fmt.Sprintf("%s %s, %s.id %s", qualifiedCol, sortOrder, tbl, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("%s.id %s", tbl, sortOrder)
	}

	var rows []map[string]any
	if err := query.Order(orderClause).Limit(limit + 1).Find(&rows).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list filtered resources from %s: %w", tableName, err)
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
			id, fmt.Sprint(row["type_slug"]), fmt.Sprint(row["status"]),
			json.RawMessage(fmt.Sprint(row["data"])),
			toString(row["created_by"]), toString(row["account_id"]),
			parseTime(row["created_at"]), toInt(row["sequence_no"]),
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
		Data: result, Cursor: nextCursor, Limit: limit, HasMore: hasMore,
	}, nil
}

func (r *ResourceRepository) FindAllByTypeFlat(
	ctx context.Context, typeSlug, cursor string, limit int,
	sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[map[string]any], error) {
	if !r.projMgr.HasProjectionTable(typeSlug) {
		return repositories.PaginatedResponse[map[string]any]{}, fmt.Errorf("no projection table for %q", typeSlug)
	}
	return r.findAllFlatFromProjection(ctx, typeSlug, nil, cursor, limit, sort, scope)
}

func (r *ResourceRepository) FindAllByTypeFlatWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[map[string]any], error) {
	if !r.projMgr.HasProjectionTable(typeSlug) {
		return repositories.PaginatedResponse[map[string]any]{}, fmt.Errorf("no projection table for %q", typeSlug)
	}
	return r.findAllFlatFromProjection(ctx, typeSlug, filters, cursor, limit, sort, scope)
}

// findAllFlatFromProjection queries the projection table directly and returns flat rows
// with all columns (including _display). No JOIN with the resources table.
// Column names are converted from snake_case to camelCase for JSON API responses.
func (r *ResourceRepository) findAllFlatFromProjection(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[map[string]any], error) {
	tableName := r.projMgr.TableName(typeSlug)
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := utils.CamelToSnake(sortBy)

	if colName != "id" && !standardColumnNames[colName] {
		if !r.db.Migrator().HasColumn(tableName, colName) {
			colName = "id"
		}
	}

	query := r.db.WithContext(ctx).Table(tableName).Where("1=1")
	query = applyVisibilityScope(query, scope, "")

	for _, f := range filters {
		sqlOp, ok := operatorMap[f.Operator]
		if !ok {
			continue
		}
		fc := utils.CamelToSnake(f.Field)
		if !standardColumnNames[fc] && fc != "id" {
			if !r.db.Migrator().HasColumn(tableName, fc) {
				continue
			}
		}
		query = query.Where(fc+" "+sqlOp+" ?", f.Value)
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

	var rows []map[string]any
	if err := query.Order(orderClause).Limit(limit + 1).Find(&rows).Error; err != nil {
		return repositories.PaginatedResponse[map[string]any]{},
			fmt.Errorf("failed to list flat resources from %s: %w", tableName, err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	// Convert snake_case keys to camelCase and build cursor.
	result := make([]map[string]any, 0, len(rows))
	var nextCursor string
	for _, row := range rows {
		camelRow := make(map[string]any, len(row))
		for k, v := range row {
			camelRow[utils.SnakeToCamel(k)] = v
		}
		result = append(result, camelRow)
		sortVal := row[colName]
		if sortVal == nil {
			sortVal = row["id"]
		}
		nextCursor = encodeCursor(fmt.Sprint(sortVal), fmt.Sprint(row["id"]))
	}
	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[map[string]any]{
		Data: result, Cursor: nextCursor, Limit: limit, HasMore: hasMore,
	}, nil
}

func (r *ResourceRepository) findAllFromGenericWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := sortBy
	if !genericSortableColumns[colName] {
		colName = "id"
	}

	query := r.db.WithContext(ctx).Model(&models.Resource{}).
		Where("type_slug = ?", typeSlug)
	query = applyVisibilityScope(query, scope, "")

	for _, f := range filters {
		sqlOp, ok := operatorMap[f.Operator]
		if !ok {
			continue
		}
		query = query.Where("json_extract(data, ?) "+sqlOp+" ?", "$."+f.Field, f.Value)
	}

	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, colName, sortOrder, cd)
		}
	}

	var dbModels []models.Resource
	orderClause := fmt.Sprintf("%s %s, id %s", colName, sortOrder, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("id %s", sortOrder)
	}
	if err := query.Order(orderClause).Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list filtered resources: %w", err)
	}

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
		nextCursor = encodeCursor(m.ID, m.ID)
	}
	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Resource]{
		Data: result, Cursor: nextCursor, Limit: limit, HasMore: hasMore,
	}, nil
}

func (r *ResourceRepository) Update(
	ctx context.Context, entity *entities.Resource,
) error {
	// Always update the generic resources table.
	model := models.FromResource(entity)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update resource: %w", err)
	}
	// Also update the projection table if one exists.
	if r.projMgr.HasProjectionTable(entity.TypeSlug()) {
		if err := r.updateProjection(ctx, entity); err != nil {
			return err
		}
	}
	return nil
}

func (r *ResourceRepository) updateProjection(
	ctx context.Context, entity *entities.Resource,
) error {
	tableName := r.projMgr.TableName(entity.TypeSlug())
	row := map[string]any{
		"status":      entity.Status(),
		"sequence_no": entity.GetSequenceNo(),
		"updated_at":  time.Now(),
	}
	ldCtx := r.projMgr.Context(entity.TypeSlug())
	ExtractFlatColumns(entity.Data(), ldCtx, row)
	err := r.db.WithContext(ctx).Table(tableName).
		Where("id = ?", entity.GetID()).Updates(row).Error
	if err != nil {
		return fmt.Errorf("failed to update resource in %s: %w", tableName, err)
	}
	return nil
}

func (r *ResourceRepository) Delete(ctx context.Context, id string) error {
	// Always delete from the generic resources table.
	err := r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Resource{}).Error
	if err != nil {
		return fmt.Errorf("failed to delete resource: %w", err)
	}
	// Also delete from the projection table if one exists.
	typeSlug := identity.ExtractResourceTypeSlug(id)
	if typeSlug != "" && r.projMgr.HasProjectionTable(typeSlug) {
		if err := r.deleteFromProjection(ctx, id, typeSlug); err != nil {
			return err
		}
	}
	return nil
}

func (r *ResourceRepository) deleteFromProjection(
	ctx context.Context, id, typeSlug string,
) error {
	tableName := r.projMgr.TableName(typeSlug)
	err := r.db.WithContext(ctx).Table(tableName).
		Where("id = ?", id).Delete(map[string]any{}).Error
	if err != nil {
		return fmt.Errorf("failed to delete resource from %s: %w", tableName, err)
	}
	return nil
}
