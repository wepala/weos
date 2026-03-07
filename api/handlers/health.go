package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// HealthHandler returns a 200 OK with a JSON status body.
func HealthHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}
