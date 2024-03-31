package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"net/http"
)

type RouteParams struct {
	fx.In
	Config             *openapi3.T
	Echo               *echo.Echo
	Logger             Log
	CommandDispatcher  CommandDispatcher
	ResourceRepository *ResourceRepository
}

func RouteInitializer(p RouteParams) {
	//TODO read all the routes and configurations to determine which controller and
	p.Echo.Add(http.MethodGet, "/health", func(c echo.Context) error {
		return c.String(200, "OK")
	})
	p.Echo.Add(http.MethodPost, "/*", DefaultWriteController(p.Logger, p.CommandDispatcher, &ResourceRepository{}, p.Config, nil, nil))
	p.Echo.Add(http.MethodPut, "/*", DefaultWriteController(p.Logger, p.CommandDispatcher, &ResourceRepository{}, p.Config, nil, nil))
	p.Echo.Add(http.MethodPatch, "/*", DefaultWriteController(p.Logger, p.CommandDispatcher, &ResourceRepository{}, p.Config, nil, nil))

	//output registered endpoints for debugging purposes
	for _, route := range p.Echo.Routes() {
		p.Logger.Debugf("Registered routes '%s' '%s'", route.Method, route.Path)
	}
}
