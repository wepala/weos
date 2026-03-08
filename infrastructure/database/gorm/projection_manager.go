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
	"strings"
	"sync"
	"unicode"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/infrastructure/models"

	"github.com/jinzhu/inflection"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

type columnDef struct {
	Name    string
	SQLType string
}

// standardColumns are always present in every projection table.
var standardColumnNames = map[string]bool{
	"id":          true,
	"type_slug":   true,
	"data":        true,
	"status":      true,
	"sequence_no": true,
	"created_at":  true,
	"updated_at":  true,
	"deleted_at":  true,
}

// jsonLDKeys are skipped when extracting columns from JSON Schema.
var jsonLDKeys = map[string]bool{
	"@id":      true,
	"@type":    true,
	"@context": true,
}

type projectionManager struct {
	db     *gorm.DB
	logger entities.Logger
	tables sync.Map // slug → tableName
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

func (pm *projectionManager) EnsureTable(ctx context.Context, slug string, schema json.RawMessage) error {
	tableName := slugToTableName(slug)

	columns := schemaToColumns(schema)

	if err := pm.createTableIfNotExists(ctx, tableName, columns); err != nil {
		return fmt.Errorf("failed to ensure projection table %q: %w", tableName, err)
	}

	if err := pm.addMissingColumns(ctx, tableName, columns); err != nil {
		return fmt.Errorf("failed to add columns to %q: %w", tableName, err)
	}

	pm.tables.Store(slug, tableName)
	return nil
}

func (pm *projectionManager) HasProjectionTable(slug string) bool {
	_, ok := pm.tables.Load(slug)
	return ok
}

func (pm *projectionManager) TableName(slug string) string {
	if v, ok := pm.tables.Load(slug); ok {
		return v.(string)
	}
	return slugToTableName(slug)
}

func (pm *projectionManager) EnsureExistingTables(ctx context.Context) error {
	var types []models.ResourceType
	if err := pm.db.WithContext(ctx).Where("deleted_at IS NULL").Find(&types).Error; err != nil {
		return fmt.Errorf("failed to load existing resource types: %w", err)
	}

	for _, rt := range types {
		schema := json.RawMessage(nil)
		if rt.Schema != "" {
			schema = json.RawMessage(rt.Schema)
		}
		if err := pm.EnsureTable(ctx, rt.Slug, schema); err != nil {
			pm.logger.Error(ctx, "failed to ensure projection table for existing type",
				"slug", rt.Slug, "error", err)
		}
	}
	return nil
}

func (pm *projectionManager) createTableIfNotExists(ctx context.Context, tableName string, columns []columnDef) error {
	dialect := pm.db.Dialector.Name()

	var colDefs []string
	colDefs = append(colDefs, "id TEXT PRIMARY KEY")
	colDefs = append(colDefs, "type_slug TEXT NOT NULL")
	colDefs = append(colDefs, "data TEXT")
	colDefs = append(colDefs, "status TEXT NOT NULL DEFAULT 'active'")
	colDefs = append(colDefs, "sequence_no INTEGER")

	if dialect == "postgres" {
		colDefs = append(colDefs, "created_at TIMESTAMP WITH TIME ZONE")
		colDefs = append(colDefs, "updated_at TIMESTAMP WITH TIME ZONE")
		colDefs = append(colDefs, "deleted_at TIMESTAMP WITH TIME ZONE")
	} else {
		colDefs = append(colDefs, "created_at DATETIME")
		colDefs = append(colDefs, "updated_at DATETIME")
		colDefs = append(colDefs, "deleted_at DATETIME")
	}

	for _, col := range columns {
		colDefs = append(colDefs, fmt.Sprintf("%s %s", col.Name, col.SQLType))
	}

	ddl := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)",
		tableName, strings.Join(colDefs, ",\n  "))

	if err := pm.db.WithContext(ctx).Exec(ddl).Error; err != nil {
		return err
	}

	idxDDL := fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_deleted_at ON %s (deleted_at)",
		tableName, tableName)
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
func schemaToColumns(schema json.RawMessage) []columnDef {
	if len(schema) == 0 {
		return nil
	}

	var s struct {
		Properties map[string]struct {
			Type string `json:"type"`
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

		colName := camelToSnake(propName)
		if standardColumnNames[colName] {
			continue
		}

		sqlType := jsonTypeToSQL(propDef.Type)
		cols = append(cols, columnDef{Name: colName, SQLType: sqlType})
	}
	return cols
}

// camelToSnake converts camelCase or PascalCase to snake_case.
func camelToSnake(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := rune(s[i-1])
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					result = append(result, '_')
				}
			}
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}
	return string(result)
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
// Skips JSON-LD meta-keys and standard column names.
func ExtractFlatColumns(data json.RawMessage, row map[string]any) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	for key, val := range m {
		if jsonLDKeys[key] {
			continue
		}
		colName := camelToSnake(key)
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
