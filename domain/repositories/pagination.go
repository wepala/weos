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

// FilterCondition represents a single filter clause for resource queries.
type FilterCondition struct {
	Field    string // camelCase field name (e.g. "courseInstanceId")
	Operator string // one of: eq, ne, gt, gte, lt, lte
	Value    string // the filter value
	// DocumentProperty compares the property in the resource's stored JSON-LD
	// document instead of the projection column named Field. It is for a schema
	// property whose name a standard column shadows — a task's "status" is the
	// task's own state, while the projection's status column is the resource
	// lifecycle — so the projection never stores the property as a column.
	// The REST listing never sets it.
	DocumentProperty bool
	// ValueType is the JSON type a DocumentProperty condition compares Value
	// as. A stored document keeps a number or a boolean as one, so a text
	// comparison never matches it: "3" is not the number 3. Empty compares as a
	// string. A projection column condition ignores it.
	ValueType FilterValueType
	// NeIncludesMissing makes ne also match a resource that has no value for
	// Field. SQL's `col != ?` is never true for NULL, so without it ne drops
	// every resource that leaves the field empty. The REST listing never sets
	// it, so its results are unchanged.
	NeIncludesMissing bool
}

// FilterValueType names the JSON type a FilterCondition's Value holds.
type FilterValueType string

// The JSON types a FilterCondition's Value can hold.
const (
	FilterValueString  FilterValueType = "string"
	FilterValueNumber  FilterValueType = "number"
	FilterValueBoolean FilterValueType = "boolean"
)

// PaginatedResponse represents a paginated response with cursor-based pagination.
type PaginatedResponse[T any] struct {
	// Data contains the paginated items.
	Data []T

	// Cursor is the cursor for the next page. Empty string indicates no more pages.
	Cursor string

	// Limit is the number of items per page.
	Limit int

	// HasMore indicates whether there are more items available beyond the current page.
	HasMore bool
}
