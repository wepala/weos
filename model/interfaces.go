package model

//go:generate moq -out temp_mocks_test.go -pkg model_test . GormProjection
import (
	"database/sql"
	"github.com/casbin/casbin/v2"
	"github.com/getkin/kin-openapi/openapi3"
	ds "github.com/ompluscator/dynamic-struct"
	"net/http"
	"time"

	"golang.org/x/net/context"
	"gorm.io/gorm"
)

type CommandDispatcher interface {
	Dispatch(ctx context.Context, command *Command, container Container, repository EntityRepository, logger Log) (interface{}, error)
	AddSubscriber(command *Command, handler CommandHandler) map[string][]CommandHandler
	GetSubscribers() map[string][]CommandHandler
}

type EventDispatcher interface {
	AddSubscriber(handler EventHandler)
	GetSubscribers() []EventHandler
	Dispatch(ctx context.Context, event Event)
}

type WeOSEntity interface {
	Entity
	GetUser() User
	SetUser(User)
}
type Entity interface {
	ValueObject
	GetID() string
}

type ValueObject interface {
	IsValid() bool
	AddError(err error)
	GetErrors() []error
}

type EventSourcedEntity interface {
	Entity
	NewChange(event *Event)
	GetNewChanges() []Entity
}

type Reducer func(initialState Entity, event Event, next Reducer) Entity

type Repository interface {
	Persist(entities []Entity) error
	Remove(entities []Entity) error
}

type UnitOfWorkRepository interface {
	Flush() error
}

type EventRepository interface {
	UnitOfWorkRepository
	Migrate(ctx context.Context) error
	Persist(ctxt context.Context, entity AggregateInterface) error
	GetByAggregate(ID string) ([]*Event, error)
	//GetByEntityAndAggregate returns events by entity id and type withing the context of the root aggregate
	GetByEntityAndAggregate(entityID string, entityType string, rootID string) ([]*Event, error)
	//GetByAggregateAndType returns events given the entity id and the entity type.
	//Deprecated: 08/12/2021 This was in theory returning events by entity (not necessarily root aggregate). Upon introducing the RootID
	//events should now be retrieved by root id,entity type and entity id. Use GetByEntityAndAggregate instead
	GetByAggregateAndType(ID string, entityType string) ([]*Event, error)
	//GetAggregateSequenceNumber returns the latest sequence number for all events in the context of the root aggregate
	GetAggregateSequenceNumber(ID string) (int64, error)
	//GetByAggregateAndSequenceRange this returns a sequence of events.
	GetByAggregateAndSequenceRange(ID string, start int64, end int64) ([]*Event, error)
	AddSubscriber(handler EventHandler)
	GetSubscribers() ([]EventHandler, error)
	ReplayEvents(ctxt context.Context, date time.Time, entityFactories map[string]EntityFactory, projection Projection, schema *openapi3.Swagger) (int, int, int, []error)
}

type Datastore interface {
	Migrate(ctx context.Context, schema *openapi3.Swagger) error
}

type Projection interface {
	Datastore
	GetEventHandler() EventHandler
	GetContentEntity(ctx context.Context, entityFactory EntityFactory, weosID string) (*ContentEntity, error)
	GetByKey(ctxt context.Context, entityFactory EntityFactory, identifiers map[string]interface{}) (*ContentEntity, error)
	//GetList returns a paginated result of content entities
	GetList(ctx context.Context, entityFactory EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]*ContentEntity, int64, error)
	GetByProperties(ctxt context.Context, entityFactory EntityFactory, identifiers map[string]interface{}) ([]*ContentEntity, error)
}

//EntityRepository is a repository that can be used to store and create entities
type EntityRepository interface {
	Projection
	EntityFactory
	Repository
	//GenerateID generates a new id for the entity IF the database doesn't support id generation for that identifier
	GenerateID(entity *ContentEntity) (*ContentEntity, error)
	//Delete deletes an entity from the repository
	Delete(ctxt context.Context, entity *ContentEntity) error
}

type GormProjection interface {
	Projection
	DB() *gorm.DB
}

type Container interface {
	//RegisterEventStore Add event store so that it can be referenced in the OpenAPI spec
	RegisterEventStore(name string, repository EventRepository)
	//GetEventStore get event dispatcher by name
	GetEventStore(name string) (EventRepository, error)
	//RegisterCommandDispatcher Add command dispatcher so that it can be referenced in the OpenAPI spec
	RegisterCommandDispatcher(name string, dispatcher CommandDispatcher)
	//GetCommandDispatcher get event dispatcher by name
	GetCommandDispatcher(name string) (CommandDispatcher, error)
	//RegisterEntityFactory Adds entity factory so that it can be referenced in the OpenAPI spec
	RegisterEntityFactory(name string, factory EntityFactory)
	//GetEntityFactory get entity factory
	GetEntityFactory(name string) (EntityFactory, error)
	//GetEntityFactories get event factories
	GetEntityFactories() map[string]EntityFactory
	//RegisterProjection Add projection so that it can be referenced in the OpenAPI spec
	RegisterProjection(name string, projection Projection)
	//GetProjection projection by name
	GetProjection(name string) (Projection, error)
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
	//RegisterLog set logger
	RegisterLog(name string, logger Log)
	//GetLog
	GetLog(name string) (Log, error)
	//RegisterHTTPClient setup http client to use
	RegisterHTTPClient(name string, client *http.Client)
	//GetHTTPClient return htpt client
	GetHTTPClient(name string) (*http.Client, error)
	//RegisterPermissionEnforcer save permission enforcer
	RegisterPermissionEnforcer(name string, enforcer *casbin.Enforcer)
	//GetPermissionEnforcer get Casbin enforcer
	GetPermissionEnforcer(name string) (*casbin.Enforcer, error)
	RegisterEntityRepository(name string, repository EntityRepository)
	GetEntityRepository(name string) (EntityRepository, error)
}

type EntityFactory interface {
	FromSchemaAndBuilder(string, *openapi3.Schema, ds.Builder) EntityFactory
	NewEntity(ctx context.Context) (*ContentEntity, error)
	//CreateEntityWithValues add an entity for the first type to the system with the following values
	CreateEntityWithValues(ctx context.Context, payload []byte) (*ContentEntity, error)
	DynamicStruct(ctx context.Context) ds.DynamicStruct
	Name() string
	TableName() string
	Schema() *openapi3.Schema
	Builder(ctx context.Context) ds.Builder
}
