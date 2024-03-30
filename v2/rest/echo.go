package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"net/http"
)

type EchoParam struct {
	fx.In
	Config *openapi3.T
}

func NewEcho() (*echo.Echo, error) {
	instance := echo.New()
	instance.Add(http.MethodGet, "/health", func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})
	return instance, nil
}
