package model

//go:generate moq -out temp_mocks_test.go -pkg model_test . GormProjection
import (
	"github.com/getkin/kin-openapi/openapi3"
	"time"

	"golang.org/x/net/context"
	"gorm.io/gorm"
)

type CommandDispatcher interface {
	Dispatch(ctx context.Context, command *Command, eventStore EventRepository, projection Projection, logger Log) error
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
	ReplayEvents(ctxt context.Context, date time.Time, entityFactories map[string]EntityFactory, projection Projection) (int, int, int, []error)
}

type Datastore interface {
	Migrate(ctx context.Context, schema *openapi3.Swagger) error
}

type Projection interface {
	Datastore
	GetEventHandler() EventHandler
	GetContentEntity(ctx context.Context, entityFactory EntityFactory, weosID string) (*ContentEntity, error)
	GetByKey(ctxt context.Context, entityFactory EntityFactory, identifiers map[string]interface{}) (*ContentEntity, error)
	//Deprecated: 03/05/2022 should use GetContentEntity
	GetByEntityID(ctxt context.Context, entityFactory EntityFactory, id string) (map[string]interface{}, error)
	//Deprecated: 05/08/2002 should use GetList instead
	GetContentEntities(ctx context.Context, entityFactory EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]map[string]interface{}, int64, error)
	//GetList returns a paginated result of content entities
	GetList(ctx context.Context, entityFactory EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]*ContentEntity, int64, error)
	GetByProperties(ctxt context.Context, entityFactory EntityFactory, identifiers map[string]interface{}) ([]*ContentEntity, error)
}

type GormProjection interface {
	Projection
	DB() *gorm.DB
}
