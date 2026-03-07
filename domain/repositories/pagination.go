package repositories

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
