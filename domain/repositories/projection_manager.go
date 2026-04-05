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

package repositories

import (
	"context"
	"encoding/json"
)

// ProjectionManager manages per-resource-type projection tables.
// When a resource type is created, a dedicated table is created with columns
// derived from the resource type's JSON Schema.
type ProjectionManager interface {
	// EnsureTable creates or updates a projection table for a resource type.
	// Idempotent: creates table if missing, adds new columns if schema changed.
	// Also caches the JSON-LD context for use by ExtractFlatColumns.
	EnsureTable(ctx context.Context, slug string, schema, ldContext json.RawMessage) error

	// HasProjectionTable reports whether a projection table exists for the slug.
	HasProjectionTable(slug string) bool

	// TableName returns the SQL table name for a given resource type slug.
	TableName(slug string) string

	// Context returns the cached JSON-LD context for a resource type slug.
	Context(slug string) json.RawMessage

	// EnsureExistingTables creates projection tables for all existing resource types.
	// Called at startup.
	EnsureExistingTables(ctx context.Context) error

	// UpdateColumn updates a single column value in a projection table row.
	// Used by triple-to-projection sync to populate FK columns from triple events.
	UpdateColumn(ctx context.Context, typeSlug, resourceID, column string, value any) error

	// UpdateColumnByFK updates a target column for all rows where a FK column matches a value.
	// Used to propagate display value changes when a referenced entity is updated.
	UpdateColumnByFK(ctx context.Context, typeSlug, fkColumn, fkValue, targetColumn string, targetValue any) error

	// ReverseReferences returns the list of resource types that reference a given target type.
	// Each entry describes a FK column and its corresponding display column.
	ReverseReferences(targetTypeSlug string) []ReverseReference

	// RegisterSubtype registers a concrete child type as a subtype of an abstract parent.
	// This merges the child's schema columns into the parent's projection table and
	// records the child-to-parent mapping so that TableName resolution routes the child
	// to the parent's table. The parent table must already exist via EnsureTable.
	RegisterSubtype(ctx context.Context, childSlug, parentSlug string, childSchema json.RawMessage) error

	// IsSubtype reports whether a slug is registered as a subtype of an abstract parent.
	IsSubtype(slug string) bool

	// ParentSlug returns the abstract parent slug for a subtype, or empty string if not a subtype.
	ParentSlug(slug string) string
}

// ReverseReference describes a resource type that references another via a FK column.
type ReverseReference struct {
	TypeSlug        string // the resource type that holds the FK
	FKColumn        string // e.g. "course_id"
	DisplayColumn   string // e.g. "course_id_display"
	DisplayProperty string // e.g. "name" — which property on the target to denormalize
}
