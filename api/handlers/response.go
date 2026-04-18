package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/wepala/weos/v3/domain/entities"

	"github.com/labstack/echo/v4"
)

// Envelope wraps a single-entity API response with an optional messages array.
type Envelope struct {
	Data     any                `json:"data"`
	Messages []entities.Message `json:"messages,omitempty"`
}

// PaginatedEnvelope wraps a paginated API response.
type PaginatedEnvelope struct {
	Data     any                `json:"data"`
	Cursor   string             `json:"cursor"`
	HasMore  bool               `json:"has_more"`
	Messages []entities.Message `json:"messages,omitempty"`
}

// ErrorEnvelope wraps an error API response. The "error" key is kept for
// backward compatibility; the messages array provides the structured form.
type ErrorEnvelope struct {
	Error    string             `json:"error"`
	Messages []entities.Message `json:"messages,omitempty"`
}

// respond sends a JSON response wrapped in the standard envelope.
// Messages accumulated on the request context are included automatically.
func respond(c echo.Context, status int, data any) error {
	msgs := entities.GetMessages(c.Request().Context())
	return c.JSON(status, Envelope{Data: data, Messages: msgs})
}

// respondRaw sends a raw JSON blob wrapped in the standard envelope.
// Use this for pre-serialized data (e.g., JSON-LD) to avoid double-encoding.
func respondRaw(c echo.Context, status int, raw json.RawMessage) error {
	msgs := entities.GetMessages(c.Request().Context())
	return c.JSON(status, Envelope{Data: raw, Messages: msgs})
}

// respondPaginated sends a paginated JSON response in the standard envelope.
func respondPaginated(
	c echo.Context, status int, data any, cursor string, hasMore bool,
) error {
	msgs := entities.GetMessages(c.Request().Context())
	return c.JSON(status, PaginatedEnvelope{
		Data:     data,
		Cursor:   cursor,
		HasMore:  hasMore,
		Messages: msgs,
	})
}

// respondError sends an error JSON response in the standard envelope.
// The error text is in the top-level "error" field for backward compatibility.
// The "messages" array contains only context-accumulated messages (e.g., warnings
// from services), not the error itself, to avoid duplication on the frontend.
func respondError(c echo.Context, status int, msg string) error {
	msgs := entities.GetMessages(c.Request().Context())
	return c.JSON(status, ErrorEnvelope{Error: msg, Messages: msgs})
}

// respondForbidden is a shorthand for 403 Forbidden responses.
func respondForbidden(c echo.Context) error {
	return respondError(c, http.StatusForbidden, "access denied")
}
