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
	//RegisterMiddleware Add middleware so that it can be referenced in the OpenAPI spec
	RegisterMiddleware(name string, middleware Middleware)
	//GetMiddleware get middleware by name
	GetMiddleware(name string) (Middleware, error)
	//RegisterController Add controller so that it can be referenced in the OpenAPI spec
	RegisterController(name string, controller Controller)
	//GetController get controller by name
	GetController(name string) (Controller, error)
	//RegisterEventStore Add event store so that it can be referenced in the OpenAPI spec
	RegisterEventStore(name string, repository model.EventRepository)
	//GetEventStore get event dispatcher by name
	GetEventStore(name string) (model.EventRepository, error)
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
	//RegisterCommandDispatcher Add command dispatcher so that it can be referenced in the OpenAPI spec
	RegisterCommandDispatcher(name string, dispatcher model.CommandDispatcher)
	//GetCommandDispatcher get event dispatcher by name
	GetCommandDispatcher(name string) (model.CommandDispatcher, error)
	//RegisterProjection Add projection so that it can be referenced in the OpenAPI spec
	RegisterProjection(name string, projection projections.Projection)
	//GetProjection projection by name
	GetProjection(name string) (projections.Projection, error)
	//RegisterEntityFactory Adds entity factory so that it can be referenced in the OpenAPI spec
	RegisterEntityFactory(name string, factory model.EntityFactory)
	//GetEntityFactory get entity factory
	GetEntityFactory(name string) (model.EntityFactory, error)
	//GetEntityFactories get event factories
	GetEntityFactories() map[string]model.EntityFactory
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
}
