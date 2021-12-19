package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos-service/model"
)

type (
	//OperationMiddleware Middleware that is bound to an OpenAPI operation
	OperationMiddleware func(model.Application, *openapi3.Operation, *openapi3.PathItem, *openapi3.Swagger) echo.MiddlewareFunc
	//OperationController is the handler for a specific operation
	OperationController func(model.Application, *openapi3.Operation, *openapi3.PathItem, *openapi3.Swagger) echo.HandlerFunc
)
