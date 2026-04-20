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

	// ForwardReferences returns the list of outgoing references on a resource type — i.e. for every
	// x-resource-type property on its schema, the FK column name, the sibling display column,
	// the target type slug, and the display property to read from the target.
	// Used during projection writes to look up display values for newly inserted rows.
	ForwardReferences(typeSlug string) []ForwardReference

	// HasColumn reports whether a projection table has a specific column.
	// Uses the column set cached during EnsureTable for fast lookup without DB queries.
	HasColumn(slug, column string) bool

	// AncestorSlugs returns the ordered chain of ancestor type slugs for a type,
	// derived from rdfs:subClassOf declarations cached during EnsureTable.
	// For "loan" with subClassOf "financial-instrument", returns ["financial-instrument"].
	// Returns nil for types with no parent. Circular references are safely broken.
	AncestorSlugs(slug string) []string

	// RegisterLink activates a cross-type link declared outside a source type's
	// schema (see application.PresetLinkDefinition). It adds the <PropertyName>
	// FK column and sibling <PropertyName>_display column to the source type's
	// projection table, then records the same ForwardReference/ReverseReference
	// entries that x-resource-type properties produce — so downstream display
	// propagation and triple extraction treat link-declared references
	// identically to schema-declared ones.
	//
	// Idempotent: calling RegisterLink twice with the same LinkReference is a
	// no-op after the first call (column exists + maps dedup on conflict).
	// Returns an error only if the ALTER TABLE fails; callers that discover
	// the source type doesn't exist yet (no projection table) should skip
	// activation and retry after the source type is installed.
	RegisterLink(ctx context.Context, ref LinkReference) error
}

// LinkReference is the cross-type-link equivalent of a ForwardReference,
// carried into ProjectionManager.RegisterLink as a single value so callers
// can't accidentally swap source and target slugs (both are strings, both
// were positional in an earlier API). Empty DisplayProperty is treated as
// "name" by the implementation.
type LinkReference struct {
	SourceSlug      string
	PropertyName    string
	TargetSlug      string
	DisplayProperty string
}

// LinkSource lets ProjectionManager re-apply link-declared references whenever
// it re-parses a source type's schema. A schema edit triggers EnsureTable,
// which clears the slug's forward/reverse refs to drop stale entries — without
// a LinkSource, link-declared refs would be wiped on every schema edit and
// only restored on the next LinkActivator.Reconcile. Implemented by the
// application's LinkRegistry; optional for the projection manager.
type LinkSource interface {
	LinkReferencesForSource(sourceSlug string) []LinkReference
}

// ReverseReference describes a resource type that references another via a FK column.
// It is the inbound view of a schema reference: "who points at me and through
// which column?". Populated alongside ForwardReference from x-resource-type
// schema properties. Use ReferencingTypeSlug (not the removed TypeSlug field)
// to identify the type holding the FK.
type ReverseReference struct {
	ReferencingTypeSlug string // the resource type that holds the FK
	FKColumn            string // e.g. "course_id"
	DisplayColumn       string // e.g. "course_id_display"
	DisplayProperty     string // e.g. "name" — which property on the target to denormalize
}

// ForwardReference describes an outgoing reference from a resource type to another.
// It is the symmetric counterpart of ReverseReference keyed on the referencing
// type. Use TargetTypeSlug to identify the referenced type. Populated alongside
// ReverseReference from x-resource-type schema properties and used to populate
// display columns on newly inserted/updated projection rows.
type ForwardReference struct {
	FKColumn        string // e.g. "course_id" on the referencing table
	DisplayColumn   string // e.g. "course_id_display" on the referencing table
	TargetTypeSlug  string // e.g. "course" — the type being referenced
	DisplayProperty string // e.g. "name" — property to read from the referenced row
}
