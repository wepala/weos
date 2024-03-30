package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
)

type RouteParams struct {
	Config *openapi3.T
	Echo   *echo.Echo
}

func RouteInitializer(techo *echo.Echo, config *openapi3.T) {
	//TODO read all the routes and configurations to determine which controller and
	techo.Add("GET", "/health", func(c echo.Context) error {
		return c.String(200, "OK")
	})
}
