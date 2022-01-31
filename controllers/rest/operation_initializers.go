package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	weoscontext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
)

const MIDDLEWARES weoscontext.ContextKey = "_middlewares"
const CONTROLLER weoscontext.ContextKey = "_controller"

//GlobalInitializer This will setup global middleware
func GlobalInitializer(pathContext context.Context, ctxt context.Context, api *RESTAPI, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) {

}

//EntityFactoryInitializer setups the EntityFactory for a specific route
func EntityFactoryInitializer(pathContext context.Context, ctxt context.Context, api *RESTAPI, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) {

}

//UserDefinedInitializer adds user defined middleware, controller, command dispatchers and event store to the initialize context
func UserDefinedInitializer(pathContext context.Context, ctxt context.Context, api *RESTAPI, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) {

}

//StandardInitializer adds standard controller and middleware if not already setup
func StandardInitializer(pathContext context.Context, ctxt context.Context, api *RESTAPI, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) {

}

//RouteInitializer creates route using information in the initialization context
func RouteInitializer(pathContext context.Context, ctxt context.Context, api *RESTAPI, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) {

}
