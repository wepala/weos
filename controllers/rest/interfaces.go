package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos-service/model"
)

type (
	//Middleware that is bound to an OpenAPI operation
	Middleware func(model.Service, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) echo.MiddlewareFunc
	//Controller is the handler for a specific operation
	Controller func(model.Service, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) echo.HandlerFunc
)
