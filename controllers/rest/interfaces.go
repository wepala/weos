package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/model"
	"golang.org/x/net/context"
)

type (
	//Middleware that is bound to an OpenAPI operation
	Middleware func(model.Service, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) echo.MiddlewareFunc
	//Controller is the handler for a specific operation
	Controller func(model.Service, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) echo.HandlerFunc
	//OperationInitializer initialzers that are run when processing OpenAPI operations
	OperationInitializer func(context.Context, context.Context, *RESTAPI, string, string, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation)
	PathInitializer      func(context.Context, *RESTAPI, string, *openapi3.Swagger, *openapi3.PathItem)
)
