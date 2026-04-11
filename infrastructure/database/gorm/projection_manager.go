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
	columns map[string]bool // cached column names for fast lookup
}

type projectionManager struct {
	db          *gorm.DB
	logger      entities.Logger
	tables      sync.Map   // slug → tableInfo
	reverseRe   sync.Map   // targetTypeSlug → []repositories.ReverseReference
	forwardRe   sync.Map   // referencingTypeSlug → []repositories.ForwardReference
	reverseReMu sync.Mutex // guards reverseRe AND forwardRe writes (symmetric)
	parentOf    sync.Map   // slug → parentSlug (from rdfs:subClassOf, for ancestor chain)
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

	colSet := make(map[string]bool, len(columns)+len(standardColumnNames))
	for col := range standardColumnNames {
		if col != "data" { // data lives in resources table, not projection
			colSet[col] = true
		}
	}
	for _, col := range columns {
		colSet[col.Name] = true
	}
	pm.tables.Store(slug, tableInfo{name: tableName, context: ldContext, columns: colSet})
	if parentSlug := jsonld.SubClassOf(ldContext); parentSlug != "" {
		pm.parentOf.Store(slug, parentSlug)
	} else {
		pm.parentOf.Delete(slug)
	}
	pm.registerReverseReferences(slug, schema)
	return nil
}

// HasProjectionTable reports whether a projection table exists. Cached entries are not
// invalidated on type deletion; this is acceptable because type deletion is rare and
// projection tables are retained even after the type is soft-deleted (for data access).
func (pm *projectionManager) HasProjectionTable(slug string) bool {
	if _, ok := pm.tables.Load(slug); ok {
		return true
	}
	// Lazy creation: another process may have created the type after startup.
	var rt models.ResourceType
	if err := pm.db.Where("slug = ? AND deleted_at IS NULL", slug).First(&rt).Error; err != nil {
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
	if err := pm.EnsureTable(context.Background(), slug, schema, ldContext); err != nil {
		pm.logger.Warn(context.Background(), "failed to lazily create projection table",
			"slug", slug, "error", err)
		return false
	}
	return true
}

func (pm *projectionManager) TableName(slug string) string {
	if v, ok := pm.tables.Load(slug); ok {
		if info, ok := v.(tableInfo); ok {
			return info.name
		}
	}
	return slugToTableName(slug)
}

func (pm *projectionManager) Context(slug string) json.RawMessage {
	if v, ok := pm.tables.Load(slug); ok {
		if info, ok := v.(tableInfo); ok {
			return info.context
		}
	}
	return nil
}

func (pm *projectionManager) HasColumn(slug, column string) bool {
	if v, ok := pm.tables.Load(slug); ok {
		if info, ok := v.(tableInfo); ok {
			return info.columns[column]
		}
	}
	return false
}

func (pm *projectionManager) UpdateColumn(ctx context.Context, typeSlug, resourceID, column string, value any) error {
	if !pm.HasProjectionTable(typeSlug) {
		return nil
	}
	tableName := pm.TableName(typeSlug)
	if err := pm.db.WithContext(ctx).Table(tableName).
		Where("id = ?", resourceID).Update(column, value).Error; err != nil {
		return err
	}
	// Propagate to ancestor tables if the column exists there.
	// If the ancestor row doesn't exist (e.g., ancestor table added after resource creation),
	// the update is a no-op (0 rows affected), which is acceptable for display value propagation.
	for _, ancestorSlug := range pm.AncestorSlugs(typeSlug) {
		if !pm.HasProjectionTable(ancestorSlug) {
			continue
		}
		if !pm.HasColumn(ancestorSlug, column) {
			continue
		}
		aTable := pm.TableName(ancestorSlug)
		if err := pm.db.WithContext(ctx).Table(aTable).
			Where("id = ?", resourceID).Update(column, value).Error; err != nil {
			return err
		}
	}
	return nil
}

func (pm *projectionManager) UpdateColumnByFK(
	ctx context.Context, typeSlug, fkColumn, fkValue, targetColumn string, targetValue any,
) error {
	if !pm.HasProjectionTable(typeSlug) {
		return nil
	}
	tableName := pm.TableName(typeSlug)
	return pm.db.WithContext(ctx).Table(tableName).
		Where(fkColumn+" = ?", fkValue).
		Update(targetColumn, targetValue).Error
}

func (pm *projectionManager) ReverseReferences(targetTypeSlug string) []repositories.ReverseReference {
	if v, ok := pm.reverseRe.Load(targetTypeSlug); ok {
		if refs, ok := v.([]repositories.ReverseReference); ok {
			cp := make([]repositories.ReverseReference, len(refs))
			copy(cp, refs)
			return cp
		}
	}
	return nil
}

func (pm *projectionManager) ForwardReferences(typeSlug string) []repositories.ForwardReference {
	if v, ok := pm.forwardRe.Load(typeSlug); ok {
		if refs, ok := v.([]repositories.ForwardReference); ok {
			cp := make([]repositories.ForwardReference, len(refs))
			copy(cp, refs)
			return cp
		}
	}
	return nil
}

// AncestorSlugs returns the ordered chain of ancestor type slugs by walking
// rdfs:subClassOf relationships cached during EnsureTable.
func (pm *projectionManager) AncestorSlugs(slug string) []string {
	var chain []string
	visited := map[string]bool{slug: true}
	current := slug
	for {
		v, ok := pm.parentOf.Load(current)
		if !ok {
			break
		}
		parent, ok := v.(string)
		if !ok || parent == "" || visited[parent] {
			break
		}
		visited[parent] = true
		chain = append(chain, parent)
		current = parent
	}
	return chain
}

// registerReverseReferences parses a schema for x-resource-type properties and
// registers reverse-reference entries so that display value propagation can find
// which projection tables need updating when a target resource changes, and the
// symmetric forward-reference entries used to populate display columns on the
// referencing type's own projection row at write time.
//
// Schema-edit safety: stale entries from a previous registration of the same
// slug are *cleared* before new entries are added. Without this, removing or
// repointing an x-resource-type property would leave dangling refs in both
// maps — the old target's reverseRe bucket and the slug's forwardRe bucket
// would still claim the property exists. The clear+rebuild pass also makes
// the per-property dedup in the append helpers redundant for re-registrations
// (they now operate on a fresh state for this slug), but the helpers still
// dedup defensively against duplicate properties within a single schema.
func (pm *projectionManager) registerReverseReferences(slug string, schema json.RawMessage) {
	pm.reverseReMu.Lock()
	defer pm.reverseReMu.Unlock()

	// Clear any prior entries that name this slug — schema may have changed.
	pm.clearReferencesForSlugLocked(slug)

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
		reverseRef := repositories.ReverseReference{
			ReferencingTypeSlug: slug,
			FKColumn:            colName,
			DisplayColumn:       colName + "_display",
			DisplayProperty:     displayProp,
		}
		forwardRef := repositories.ForwardReference{
			FKColumn:        colName,
			DisplayColumn:   colName + "_display",
			TargetTypeSlug:  prop.XResourceType,
			DisplayProperty: displayProp,
		}
		pm.appendReverseRefLocked(prop.XResourceType, reverseRef)
		pm.appendForwardRefLocked(slug, forwardRef)
	}
}

// clearReferencesForSlugLocked removes every reference entry that names slug
// from both the forward and reverse maps. The forward bucket for slug is
// dropped wholesale; reverse buckets are walked and any entry whose
// ReferencingTypeSlug matches slug is filtered out (using copy-on-write so
// concurrent readers of the previous slice are unaffected). Caller must hold
// reverseReMu.
func (pm *projectionManager) clearReferencesForSlugLocked(slug string) {
	// Drop the forward bucket entirely — all forward refs for slug come from
	// its own schema, so they're all stale by definition on re-registration.
	pm.forwardRe.Delete(slug)

	// Walk reverse buckets and filter out any entries that name slug as the
	// referencer. Buckets keyed on different target types may contain refs
	// from many referencing types, so we can't drop them wholesale.
	pm.reverseRe.Range(func(key, value any) bool {
		refs, ok := value.([]repositories.ReverseReference)
		if !ok {
			return true
		}
		filtered := make([]repositories.ReverseReference, 0, len(refs))
		removed := false
		for _, r := range refs {
			if r.ReferencingTypeSlug == slug {
				removed = true
				continue
			}
			filtered = append(filtered, r)
		}
		if !removed {
			return true
		}
		if len(filtered) == 0 {
			pm.reverseRe.Delete(key)
		} else {
			pm.reverseRe.Store(key, filtered)
		}
		return true
	})
}

// appendReverseRefLocked appends a ReverseReference to the targetSlug bucket,
// replacing any existing entry keyed on (ReferencingTypeSlug, FKColumn). This
// lets a schema edit (e.g. a new x-display-property) take effect on the next
// EnsureTable instead of being silently dropped as a "duplicate". Caller must
// hold reverseReMu.
func (pm *projectionManager) appendReverseRefLocked(
	targetSlug string, ref repositories.ReverseReference,
) {
	existing, _ := pm.reverseRe.Load(targetSlug)
	var old []repositories.ReverseReference
	if existing != nil {
		old = existing.([]repositories.ReverseReference)
	}
	updated := make([]repositories.ReverseReference, 0, len(old)+1)
	for _, r := range old {
		if r.ReferencingTypeSlug == ref.ReferencingTypeSlug && r.FKColumn == ref.FKColumn {
			continue // drop stale entry — overwrite with the new one
		}
		updated = append(updated, r)
	}
	updated = append(updated, ref)
	pm.reverseRe.Store(targetSlug, updated)
}

// appendForwardRefLocked appends a ForwardReference to the referencingSlug
// bucket, replacing any existing entry keyed on (FKColumn, TargetTypeSlug).
// Overwrite-on-conflict is important: a schema edit that changes
// x-display-property from "name" to "title" must take effect on the next
// EnsureTable — otherwise the stale DisplayProperty would silently win and
// populateDisplayColumns would keep reading from the wrong field. Caller must
// hold reverseReMu.
func (pm *projectionManager) appendForwardRefLocked(
	referencingSlug string, ref repositories.ForwardReference,
) {
	existing, _ := pm.forwardRe.Load(referencingSlug)
	var old []repositories.ForwardReference
	if existing != nil {
		old = existing.([]repositories.ForwardReference)
	}
	updated := make([]repositories.ForwardReference, 0, len(old)+1)
	for _, r := range old {
		if r.FKColumn == ref.FKColumn && r.TargetTypeSlug == ref.TargetTypeSlug {
			continue // drop stale entry — overwrite with the new one
		}
		updated = append(updated, r)
	}
	updated = append(updated, ref)
	pm.forwardRe.Store(referencingSlug, updated)
}

func (pm *projectionManager) EnsureExistingTables(ctx context.Context) error {
	var types []models.ResourceType
	if err := pm.db.WithContext(ctx).
		Where("deleted_at IS NULL").Find(&types).Error; err != nil {
		return fmt.Errorf("failed to load existing resource types: %w", err)
	}
	for _, rt := range types {
		var schema json.RawMessage
		if rt.Schema != "" {
			schema = json.RawMessage(rt.Schema)
		}
		var ldContext json.RawMessage
		if rt.Context != "" {
			ldContext = json.RawMessage(rt.Context)
		}
		if err := pm.EnsureTable(ctx, rt.Slug, schema, ldContext); err != nil {
			pm.logger.Error(ctx, "failed to ensure projection table",
				"slug", rt.Slug, "error", err)
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

	return pm.db.WithContext(ctx).Exec(ddl).Error
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
