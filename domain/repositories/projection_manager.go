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
	EnsureTable(ctx context.Context, slug string, schema json.RawMessage) error

	// HasProjectionTable reports whether a projection table exists for the slug.
	HasProjectionTable(slug string) bool

	// TableName returns the SQL table name for a given resource type slug.
	TableName(slug string) string

	// EnsureExistingTables creates projection tables for all existing resource types.
	// Called at startup.
	EnsureExistingTables(ctx context.Context) error
}
