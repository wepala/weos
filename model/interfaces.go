package model

//go:generate moq -out temp_mocks_test.go -pkg model_test . Projection
import (
	weosContext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
)

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
	Datastore
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
	//Deprecated: 08/17/2021 This isn't actually used and would need to be updated to account for the new RootID property on events
	GetByAggregateAndSequenceRange(ID string, start int64, end int64) ([]*Event, error)
	AddSubscriber(handler EventHandler)
	GetSubscribers() ([]EventHandler, error)
}

type Datastore interface {
	Migrate(ctx context.Context) error
}

type Projection interface {
	Datastore
	GetEventHandler() EventHandler
	GetContentEntity(ctx context.Context, weosID string) (*ContentEntity, error)
	GetByKey(ctxt context.Context, contentType weosContext.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error)
	GetByEntityID(ctxt context.Context, contentType weosContext.ContentType, id string) (map[string]interface{}, error)
}
