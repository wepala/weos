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
	"errors"
	"fmt"
	"strings"
	"sync"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/infrastructure/models"
	"weos/pkg/jsonld"
	"weos/pkg/utils"

	"github.com/jinzhu/inflection"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

type columnDef struct {
	Name    string
	SQLType string
}

// standardColumnNames lists column names already part of the projection table DDL
// and should be skipped when extracting columns from JSON Schema.
var standardColumnNames = map[string]bool{
	"id":          true,
	"type_slug":   true,
	"data":        true,
	"status":      true,
	"created_by":  true,
	"account_id":  true,
	"sequence_no": true,
	"created_at":  true,
	"updated_at":  true,
}

// jsonLDKeys are skipped when extracting columns from JSON Schema.
var jsonLDKeys = map[string]bool{
	"@id":      true,
	"@type":    true,
	"@context": true,
}

type tableInfo struct {
	name    string
	context json.RawMessage
}

type projectionManager struct {
	db            *gorm.DB
	logger        entities.Logger
	tables        sync.Map   // slug → tableInfo
	reverseRe     sync.Map   // targetTypeSlug → []repositories.ReverseReference
	reverseReMu   sync.Mutex // guards reverseRe writes
	childToParent sync.Map   // childSlug → parentSlug (subtypes sharing parent's projection table)
	childContexts sync.Map   // childSlug → json.RawMessage (child's own JSON-LD context)
}

type ProjectionManagerResult struct {
	fx.Out
	ProjectionManager repositories.ProjectionManager
}

func ProvideProjectionManager(params struct {
	fx.In
	DB     *gorm.DB
	Logger entities.Logger
}) ProjectionManagerResult {
	return ProjectionManagerResult{
		ProjectionManager: &projectionManager{
			db:     params.DB,
			logger: params.Logger,
		},
	}
}

func (pm *projectionManager) EnsureTable(
	ctx context.Context, slug string, schema, ldContext json.RawMessage,
) error {
	tableName := slugToTableName(slug)

	columns := schemaToColumns(schema)

	if err := pm.createTableIfNotExists(ctx, tableName, columns); err != nil {
		return fmt.Errorf("failed to ensure projection table %q: %w", tableName, err)
	}

	if err := pm.addMissingColumns(ctx, tableName, columns); err != nil {
		return fmt.Errorf("failed to add columns to %q: %w", tableName, err)
	}

	pm.tables.Store(slug, tableInfo{name: tableName, context: ldContext})
	pm.registerReverseReferences(slug, schema)
	return nil
}

func (pm *projectionManager) HasProjectionTable(slug string) bool {
	if _, ok := pm.tables.Load(slug); ok {
		return true
	}
	if v, ok := pm.childToParent.Load(slug); ok {
		// Only return true if the parent's table is actually ready (not just a sentinel).
		if parentSlug, ok := v.(string); ok && parentSlug != "" {
			if _, parentReady := pm.tables.Load(parentSlug); parentReady {
				return true
			}
		}
	}
	// Lazy registration: another process may have created the type after startup.
	var rt models.ResourceType
	if err := pm.db.Where("slug = ?", slug).First(&rt).Error; err != nil {
		// TODO: add context parameter to HasProjectionTable in a future refactor.
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			pm.logger.Warn(context.Background(), "failed to look up resource type for projection",
				"slug", slug, "error", err)
		}
		return false
	}
	var schema json.RawMessage
	if rt.Schema != "" {
		schema = json.RawMessage(rt.Schema)
	}
	var ldContext json.RawMessage
	if rt.Context != "" {
		ldContext = json.RawMessage(rt.Context)
	}
	// Abstract types always get their own projection table, even if they also
	// declare rdfs:subClassOf. Check this before subtype registration.
	if jsonld.IsAbstract(ldContext) {
		if err := pm.EnsureTable(context.Background(), slug, schema, ldContext); err != nil {
			pm.logger.Warn(context.Background(), "failed to lazily create abstract projection table",
				"slug", slug, "error", err)
			return false
		}
		return true
	}
	// Check if this type is a subtype of an abstract parent.
	parentSlug := jsonld.SubClassOf(ldContext)
	if parentSlug != "" {
		if pm.lazyRegisterSubtype(slug, parentSlug, schema, ldContext) {
			return true
		}
	}
	if err := pm.EnsureTable(context.Background(), slug, schema, ldContext); err != nil {
		pm.logger.Warn(context.Background(), "failed to lazily create projection table",
			"slug", slug, "error", err)
		return false
	}
	return true
}

// lazyRegisterSubtype checks if the parent is abstract and registers the child as a subtype.
// A sentinel is stored in childToParent before recursing into HasProjectionTable to
// prevent infinite recursion when circular rdfs:subClassOf relationships exist.
func (pm *projectionManager) lazyRegisterSubtype(
	childSlug, parentSlug string, childSchema, childLdContext json.RawMessage,
) bool {
	var parent models.ResourceType
	if err := pm.db.Where("slug = ?", parentSlug).First(&parent).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			pm.logger.Warn(context.Background(), "failed to look up parent type for subtype registration",
				"child", childSlug, "parent", parentSlug, "error", err)
		}
		return false
	}
	var parentCtx json.RawMessage
	if parent.Context != "" {
		parentCtx = json.RawMessage(parent.Context)
	}
	if !jsonld.IsAbstract(parentCtx) {
		return false
	}
	// Store sentinel to break potential circular recursion.
	pm.childToParent.Store(childSlug, parentSlug)
	if !pm.HasProjectionTable(parentSlug) {
		pm.childToParent.Delete(childSlug)
		return false
	}
	if err := pm.RegisterSubtype(context.Background(), childSlug, parentSlug, childSchema, childLdContext); err != nil {
		pm.childToParent.Delete(childSlug)
		pm.logger.Warn(context.Background(), "failed to lazily register subtype",
			"child", childSlug, "parent", parentSlug, "error", err)
		return false
	}
	return true
}

func (pm *projectionManager) TableName(slug string) string {
	resolved := pm.resolveSlug(slug)
	if v, ok := pm.tables.Load(resolved); ok {
		if info, ok := v.(tableInfo); ok {
			return info.name
		}
	}
	return slugToTableName(resolved)
}

func (pm *projectionManager) Context(slug string) json.RawMessage {
	// Return the child's own context if it's a registered subtype.
	if v, ok := pm.childContexts.Load(slug); ok {
		if ctx, ok := v.(json.RawMessage); ok {
			return ctx
		}
	}
	resolved := pm.resolveSlug(slug)
	if v, ok := pm.tables.Load(resolved); ok {
		if info, ok := v.(tableInfo); ok {
			return info.context
		}
	}
	return nil
}

// resolveSlug returns the parent slug if slug is a registered subtype, otherwise slug itself.
func (pm *projectionManager) resolveSlug(slug string) string {
	if v, ok := pm.childToParent.Load(slug); ok {
		if parentSlug, ok := v.(string); ok {
			return parentSlug
		}
	}
	return slug
}

func (pm *projectionManager) UpdateColumn(ctx context.Context, typeSlug, resourceID, column string, value any) error {
	if !pm.HasProjectionTable(typeSlug) {
		return nil
	}
	tableName := pm.TableName(typeSlug)
	return pm.db.WithContext(ctx).Table(tableName).
		Where("id = ?", resourceID).
		Update(column, value).Error
}

func (pm *projectionManager) UpdateColumnByFK(
	ctx context.Context, typeSlug, fkColumn, fkValue, targetColumn string, targetValue any,
) error {
	if !pm.HasProjectionTable(typeSlug) {
		return nil
	}
	tableName := pm.TableName(typeSlug)
	query := pm.db.WithContext(ctx).Table(tableName).
		Where(fkColumn+" = ?", fkValue)
	// Add type_slug discriminator for shared tables to avoid updating other subtypes' rows.
	if pm.IsSubtype(typeSlug) {
		query = query.Where("type_slug = ?", typeSlug)
	}
	return query.Update(targetColumn, targetValue).Error
}

func (pm *projectionManager) ReverseReferences(targetTypeSlug string) []repositories.ReverseReference {
	if v, ok := pm.reverseRe.Load(targetTypeSlug); ok {
		if refs, ok := v.([]repositories.ReverseReference); ok {
			return refs
		}
	}
	return nil
}

func (pm *projectionManager) RegisterSubtype(
	ctx context.Context, childSlug, parentSlug string,
	childSchema, childLdContext json.RawMessage,
) error {
	if _, ok := pm.tables.Load(parentSlug); !ok {
		return fmt.Errorf("parent type %q has no projection table; call EnsureTable first", parentSlug)
	}
	parentTableName := pm.TableName(parentSlug)
	columns := schemaToColumns(childSchema)
	if err := pm.addMissingColumns(ctx, parentTableName, columns); err != nil {
		return fmt.Errorf("failed to add subtype columns to %q: %w", parentTableName, err)
	}
	pm.childToParent.Store(childSlug, parentSlug)
	if len(childLdContext) > 0 {
		pm.childContexts.Store(childSlug, childLdContext)
	}
	pm.registerReverseReferences(childSlug, childSchema)
	return nil
}

func (pm *projectionManager) IsSubtype(slug string) bool {
	_, ok := pm.childToParent.Load(slug)
	return ok
}

func (pm *projectionManager) ParentSlug(slug string) string {
	if v, ok := pm.childToParent.Load(slug); ok {
		if ps, ok := v.(string); ok {
			return ps
		}
	}
	return ""
}

// registerReverseReferences parses a schema for x-resource-type properties and
// registers reverse-reference entries so that display value propagation can find
// which projection tables need updating when a target resource changes.
func (pm *projectionManager) registerReverseReferences(slug string, schema json.RawMessage) {
	if len(schema) == 0 {
		return
	}
	var s struct {
		Properties map[string]struct {
			XResourceType    string `json:"x-resource-type"`
			XDisplayProperty string `json:"x-display-property"`
		} `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return
	}

	for propName, prop := range s.Properties {
		if prop.XResourceType == "" {
			continue
		}
		displayProp := prop.XDisplayProperty
		if displayProp == "" {
			displayProp = "name"
		}
		colName := utils.CamelToSnake(propName)
		ref := repositories.ReverseReference{
			TypeSlug:        slug,
			FKColumn:        colName,
			DisplayColumn:   colName + "_display",
			DisplayProperty: displayProp,
		}

		// Append to existing slice or create new, under lock to prevent TOCTOU races.
		pm.reverseReMu.Lock()
		existing, _ := pm.reverseRe.Load(prop.XResourceType)
		var refs []repositories.ReverseReference
		if existing != nil {
			refs = existing.([]repositories.ReverseReference)
		}
		found := false
		for _, r := range refs {
			if r.TypeSlug == ref.TypeSlug && r.FKColumn == ref.FKColumn {
				found = true
				break
			}
		}
		if !found {
			refs = append(refs, ref)
			pm.reverseRe.Store(prop.XResourceType, refs)
		}
		pm.reverseReMu.Unlock()
	}
}

func (pm *projectionManager) EnsureExistingTables(ctx context.Context) error {
	var types []models.ResourceType
	if err := pm.db.WithContext(ctx).Find(&types).Error; err != nil {
		return fmt.Errorf("failed to load existing resource types: %w", err)
	}

	// Build a slug→context lookup for parent resolution.
	typeBySlug := make(map[string]models.ResourceType, len(types))
	for _, rt := range types {
		typeBySlug[rt.Slug] = rt
	}

	// Pass 1: Create projection tables for abstract types and concrete types without abstract parents.
	for _, rt := range types {
		schema := json.RawMessage(nil)
		if rt.Schema != "" {
			schema = json.RawMessage(rt.Schema)
		}
		var ldContext json.RawMessage
		if rt.Context != "" {
			ldContext = json.RawMessage(rt.Context)
		}
		// If this concrete type has an abstract parent, skip — handled in pass 2.
		// Abstract types always get their own table even if they declare subClassOf.
		if !jsonld.IsAbstract(ldContext) {
			parentSlug := jsonld.SubClassOf(ldContext)
			if parentSlug != "" {
				if parent, ok := typeBySlug[parentSlug]; ok {
					var parentCtx json.RawMessage
					if parent.Context != "" {
						parentCtx = json.RawMessage(parent.Context)
					}
					if jsonld.IsAbstract(parentCtx) {
						continue
					}
				}
			}
		}
		if err := pm.EnsureTable(ctx, rt.Slug, schema, ldContext); err != nil {
			pm.logger.Error(ctx, "failed to ensure projection table for existing type",
				"slug", rt.Slug, "error", err)
		}
	}

	// Pass 2: Register concrete subtypes of abstract parents (abstract children got their own tables).
	for _, rt := range types {
		var ldContext json.RawMessage
		if rt.Context != "" {
			ldContext = json.RawMessage(rt.Context)
		}
		if jsonld.IsAbstract(ldContext) {
			continue
		}
		parentSlug := jsonld.SubClassOf(ldContext)
		if parentSlug == "" {
			continue
		}
		parent, ok := typeBySlug[parentSlug]
		if !ok {
			continue
		}
		var parentCtx json.RawMessage
		if parent.Context != "" {
			parentCtx = json.RawMessage(parent.Context)
		}
		if !jsonld.IsAbstract(parentCtx) {
			continue
		}
		schema := json.RawMessage(nil)
		if rt.Schema != "" {
			schema = json.RawMessage(rt.Schema)
		}
		if err := pm.RegisterSubtype(ctx, rt.Slug, parentSlug, schema, ldContext); err != nil {
			pm.logger.Error(ctx, "failed to register subtype projection",
				"child", rt.Slug, "parent", parentSlug, "error", err)
		}
	}
	return nil
}

func (pm *projectionManager) createTableIfNotExists(ctx context.Context, tableName string, columns []columnDef) error {
	dialect := pm.db.Name()

	var colDefs []string
	colDefs = append(colDefs, "id TEXT PRIMARY KEY")
	colDefs = append(colDefs, "type_slug TEXT NOT NULL")
	colDefs = append(colDefs, "status TEXT NOT NULL DEFAULT 'active'")
	colDefs = append(colDefs, "created_by TEXT")
	colDefs = append(colDefs, "account_id TEXT")
	colDefs = append(colDefs, "sequence_no INTEGER")

	if dialect == "postgres" {
		colDefs = append(colDefs, "created_at TIMESTAMP WITH TIME ZONE")
		colDefs = append(colDefs, "updated_at TIMESTAMP WITH TIME ZONE")
	} else {
		colDefs = append(colDefs, "created_at DATETIME")
		colDefs = append(colDefs, "updated_at DATETIME")
	}

	for _, col := range columns {
		colDefs = append(colDefs, fmt.Sprintf("%s %s", col.Name, col.SQLType))
	}

	ddl := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)",
		tableName, strings.Join(colDefs, ",\n  "))

	if err := pm.db.WithContext(ctx).Exec(ddl).Error; err != nil {
		return err
	}

	// Index on type_slug for efficient subtype queries on shared tables.
	// Truncate index name to stay under Postgres' 63-byte identifier limit.
	idxName := fmt.Sprintf("idx_%s_type_slug", tableName)
	if len(idxName) > 63 {
		idxName = idxName[:63]
	}
	idxDDL := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s (type_slug)",
		idxName, tableName)
	return pm.db.WithContext(ctx).Exec(idxDDL).Error
}

func (pm *projectionManager) addMissingColumns(ctx context.Context, tableName string, columns []columnDef) error {
	for _, col := range columns {
		if pm.db.Migrator().HasColumn(tableName, col.Name) {
			continue
		}
		ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, col.Name, col.SQLType)
		if err := pm.db.WithContext(ctx).Exec(ddl).Error; err != nil {
			return err
		}
	}
	return nil
}

// slugToTableName converts a resource type slug to a SQL table name.
// Replaces hyphens with underscores and pluralizes.
func slugToTableName(slug string) string {
	name := strings.ReplaceAll(slug, "-", "_")
	return inflection.Plural(name)
}

// schemaToColumns parses a JSON Schema and returns column definitions.
// Skips JSON-LD meta-keys and standard column names.
// For properties with x-resource-type, an additional _display column is generated.
func schemaToColumns(schema json.RawMessage) []columnDef {
	if len(schema) == 0 {
		return nil
	}

	var s struct {
		Properties map[string]struct {
			Type          string `json:"type"`
			XResourceType string `json:"x-resource-type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil
	}

	var cols []columnDef
	for propName, propDef := range s.Properties {
		if jsonLDKeys[propName] {
			continue
		}

		colName := utils.CamelToSnake(propName)
		if standardColumnNames[colName] {
			continue
		}

		sqlType := jsonTypeToSQL(propDef.Type)
		cols = append(cols, columnDef{Name: colName, SQLType: sqlType})

		// Add a denormalized display column for reference properties.
		if propDef.XResourceType != "" {
			cols = append(cols, columnDef{Name: colName + "_display", SQLType: "VARCHAR(512)"})
		}
	}
	return cols
}

// jsonTypeToSQL maps JSON Schema types to SQL column types.
func jsonTypeToSQL(jsonType string) string {
	switch jsonType {
	case "string":
		return "TEXT"
	case "number":
		return "REAL"
	case "integer":
		return "INTEGER"
	case "boolean":
		return "BOOLEAN"
	default:
		return "TEXT"
	}
}

// ExtractFlatColumns extracts flat key-value pairs from JSON data into a row map.
// Supports both @graph format and legacy flat format.
// For @graph: extracts intrinsic props from entity node, FK values from edges node.
// Skips JSON-LD meta-keys and standard column names.
func ExtractFlatColumns(data, ldContext json.RawMessage, row map[string]any) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return
	}

	// Check for @graph format.
	if graphArr, ok := doc["@graph"].([]any); ok && len(graphArr) > 0 {
		// Extract intrinsic properties from entity node (first in @graph).
		if entityNode, ok := graphArr[0].(map[string]any); ok {
			extractNodeColumns(entityNode, row)
		}
		// Extract FK values from edges node (second in @graph).
		if len(graphArr) > 1 {
			if edgesNode, ok := graphArr[1].(map[string]any); ok {
				extractEdgeColumns(edgesNode, ldContext, row)
			}
		}
		return
	}

	// Legacy flat format.
	extractNodeColumns(doc, row)
}

// extractNodeColumns extracts flat properties from a JSON-LD node into a row map.
func extractNodeColumns(m map[string]any, row map[string]any) {
	for key, val := range m {
		if jsonLDKeys[key] {
			continue
		}
		colName := utils.CamelToSnake(key)
		if standardColumnNames[colName] {
			continue
		}
		switch v := val.(type) {
		case map[string]any, []any:
			b, err := json.Marshal(v)
			if err == nil {
				row[colName] = string(b)
			}
		default:
			row[colName] = val
		}
	}
}

// extractEdgeColumns extracts FK values from a JSON-LD edges node into a row map.
// Uses the @context to reverse-map predicate IRIs back to property names,
// then converts property names to snake_case column names.
func extractEdgeColumns(edges map[string]any, ldContext json.RawMessage, row map[string]any) {
	reverseMap := jsonld.BuildReverseMap(ldContext)

	for key, val := range edges {
		if key == "@id" {
			continue
		}
		ref, ok := val.(map[string]any)
		if !ok {
			continue
		}
		objectID, ok := ref["@id"].(string)
		if !ok || objectID == "" {
			continue
		}

		// Reverse-lookup: predicate IRI → property name → snake_case column.
		propName, ok := reverseMap[key]
		if !ok {
			continue
		}
		colName := utils.CamelToSnake(propName)
		if standardColumnNames[colName] {
			continue
		}
		row[colName] = objectID
	}
}
