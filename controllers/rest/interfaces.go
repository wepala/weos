//go:generate moq -out rest_mocks_test.go -pkg rest_test . Container Validator
package rest

import (
	"database/sql"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"golang.org/x/net/context"
	"gorm.io/gorm"
)

type (
	//Middleware that is bound to an OpenAPI operation
	Middleware func(api Container, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc
	//Controller is the handler for a specific operation
	Controller func(api Container, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc
	//OperationInitializer initialzers that are run when processing OpenAPI operations
	GlobalInitializer    func(context.Context, Container, *openapi3.Swagger) (context.Context, error)
	OperationInitializer func(context.Context, Container, string, string, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) (context.Context, error)
	PathInitializer      func(context.Context, Container, string, *openapi3.Swagger, *openapi3.PathItem) (context.Context, error)
)

type Container interface {
	model.Container
	//RegisterMiddleware Add middleware so that it can be referenced in the OpenAPI spec
	RegisterMiddleware(name string, middleware Middleware)
	//GetMiddleware get middleware by name
	GetMiddleware(name string) (Middleware, error)
	//RegisterController Add controller so that it can be referenced in the OpenAPI spec
	RegisterController(name string, controller Controller)
	//GetController get controller by name
	GetController(name string) (Controller, error)
	//RegisterGlobalInitializer add global initializer if it's not already there
	RegisterGlobalInitializer(initializer GlobalInitializer)
	//GetGlobalInitializers get global intializers in the order they were registered
	GetGlobalInitializers() []GlobalInitializer
	//RegisterOperationInitializer add operation initializer if it's not already there
	RegisterOperationInitializer(initializer OperationInitializer)
	//GetOperationInitializers get operation intializers in the order they were registered
	GetOperationInitializers() []OperationInitializer
	//RegisterPrePathInitializer add path initializer that runs BEFORE operation initializers if it's not already there
	RegisterPrePathInitializer(initializer PathInitializer)
	//GetPrePathInitializers get path initializers in the order they were registered that run BEFORE the operations are processed
	GetPrePathInitializers() []PathInitializer
	//RegisterPostPathInitializer add path initializer that runs AFTER operation initializers if it's not already there
	RegisterPostPathInitializer(initializer PathInitializer)
	//GetPostPathInitializers get path intializers in the order they were registered that run AFTER the operations are processed
	GetPostPathInitializers() []PathInitializer
	//RegisterDBConnection save db connection
	RegisterDBConnection(name string, connection *sql.DB)
	//GetDBConnection get db connection by name
	GetDBConnection(name string) (*sql.DB, error)
	//RegisterGORMDB save gorm connection
	RegisterGORMDB(name string, connection *gorm.DB)
	//GetGormDBConnection get gorm connection by name
	GetGormDBConnection(name string) (*gorm.DB, error)
	//GetConfig the swagger configuration
	GetConfig() *openapi3.Swagger
	//GetWeOSConfig this is the old way of getting the config
	GetWeOSConfig() *APIConfig
	RegisterSecurityConfiguration(configuration *SecurityConfiguration)
	GetSecurityConfiguration() *SecurityConfiguration
}
