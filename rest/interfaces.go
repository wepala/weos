//go:generate moq -out rest_mocks_test.go -pkg rest_test . Log Repository Projection CommandDispatcher EventDispatcher EventStore

package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"golang.org/x/net/context"
)

type (
	//Middleware that is bound to an OpenAPI operation
	Middleware func(params *MiddlewareParams) echo.MiddlewareFunc
	//Controller is the handler for a specific operation
	Controller func(params *ControllerParams) echo.HandlerFunc
	//OperationInitializer initialzers that are run when processing OpenAPI operations
	GlobalInitializer    func(context.Context, *openapi3.T) (context.Context, error)
	OperationInitializer func(context.Context, string, string, *openapi3.T, *openapi3.PathItem, *openapi3.Operation) (context.Context, error)
	PathInitializer      func(context.Context, string, *openapi3.T, *openapi3.PathItem) (context.Context, error)
	CommandHandler       func(ctx context.Context, command *Command, logger Log, options *CommandOptions) (response CommandResponse, err error)
	EventHandler         func(ctx context.Context, logger Log, event *Event) error
)

type Entity interface {
	ValueObject
	GetID() string
	GetType() string
}

type ValueObject interface {
	IsValid() bool
	AddError(err error)
	GetErrors() []error
}

type Repository interface {
	Persist(ctxt context.Context, logger Log, resources []Resource) []error
	Remove(ctxt context.Context, logger Log, resources []Resource) []error
}

type CommandDispatcher interface {
	Dispatch(ctx context.Context, command *Command, logger Log, options *CommandOptions) (response CommandResponse, err error)
	AddSubscriber(command CommandConfig) map[string][]CommandHandler
	GetSubscribers() map[string][]CommandHandler
}

type EventDispatcher interface {
	AddSubscriber(handler EventHandlerConfig) error
	GetSubscribers(resourceType string) map[string][]EventHandler
	Dispatch(ctx context.Context, logger Log, event *Event) []error
}

type EventStore interface {
	Repository
	EventDispatcher
	Projection
}

type Projection interface {
	GetByURI(ctxt context.Context, logger Log, uri string) (Resource, error)
	// GetByKey returns a single content entity
	GetByKey(ctxt context.Context, identifiers map[string]interface{}) (Resource, error)
	// GetList returns a paginated result of content entities
	GetList(ctx context.Context, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]Resource, int64, error)
	GetByProperties(ctxt context.Context, identifiers map[string]interface{}) ([]Entity, error)
	GetEventHandlers() []EventHandlerConfig
}
