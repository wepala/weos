package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos-service/model"
	"golang.org/x/net/context"
)

type (
	//Middleware that is bound to an OpenAPI operation
	Middleware func(model.Service, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) echo.MiddlewareFunc
	//Controller is the handler for a specific operation
	Controller func(model.Service, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) echo.HandlerFunc
	//InitializationMiddleware are middleware that are used during the startup process of the service
	InitializationMiddleware func(context.Context, *echo.Echo, *openapi3.Swagger) echo.MiddlewareFunc
)
