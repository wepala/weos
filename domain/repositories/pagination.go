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
}

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
