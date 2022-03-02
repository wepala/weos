package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"golang.org/x/net/context"
)

type (
	//Middleware that is bound to an OpenAPI operation
	Middleware func(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc
	//Controller is the handler for a specific operation
	Controller func(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc
	//OperationInitializer initialzers that are run when processing OpenAPI operations
	GlobalInitializer    func(context.Context, *RESTAPI, *openapi3.Swagger) (context.Context, error)
	OperationInitializer func(context.Context, *RESTAPI, string, string, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) (context.Context, error)
	PathInitializer      func(context.Context, *RESTAPI, string, *openapi3.Swagger, *openapi3.PathItem) (context.Context, error)
)
