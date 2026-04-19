package handlers

import (
	"encoding/json"

	"github.com/labstack/echo/v4"
)

// Test exports for package-private response helpers.

func ExportRespond(c echo.Context, status int, data any) error {
	return respond(c, status, data)
}

func ExportRespondRaw(c echo.Context, status int, raw json.RawMessage) error {
	return respondRaw(c, status, raw)
}

func ExportRespondPaginated(
	c echo.Context, status int, data any, cursor string, hasMore bool,
) error {
	return respondPaginated(c, status, data, cursor, hasMore)
}

func ExportRespondError(c echo.Context, status int, msg string) error {
	return respondError(c, status, msg)
}

func ExportRespondForbidden(c echo.Context) error {
	return respondForbidden(c)
}
