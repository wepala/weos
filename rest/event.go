package rest

import (
	"errors"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/segmentio/ksuid"
	"golang.org/x/net/context"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"net/http"
	"sync"
	"time"
)

type Event struct {
	ID        string `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Type      string            `json:"type"`
	Payload   datatypes.JSONMap `json:"payload"`
	Meta      EventMeta         `json:"meta" gorm:"embedded"`
	Version   int               `json:"version"`
	errors    []error
}

type EventMeta struct {
	ResourceID    string `json:"resourceId"`
	ResourceType  string `json:"resourceType"`
	SequenceNo    int64  `json:"sequenceNo"`
	User          string `json:"user"`
	ApplicationID string `json:"applicationId"`
	RootID        string `json:"rootId"`
	AccountID     string `json:"accountId"`
	Created       string `json:"created"`
}

type EventOptions struct {
	ResourceRepository *ResourceRepository
	DefaultProjection  Projection
	Projections        map[string]Projection
	HttpClient         *http.Client
	GORMDB             *gorm.DB
	Request            *http.Request
}

func (e *Event) NewChange(event *Event) {
	//TODO implement me
	panic("implement me")
}

func (e *Event) GetNewChanges() []Resource {
	//TODO implement me
	panic("implement me")
}

func (e *Event) Persist() {
	//TODO implement me
	panic("implement me")
}

func (e *Event) GetType() string {
	return e.Type
}

func (e *Event) GetSequenceNo() int64 {
	return e.Meta.SequenceNo
}

func (e *Event) GetID() string {
	return e.ID
}

func (e *Event) FromBytes(schema *openapi3.T, data []byte) (Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (e *Event) IsValid() bool {
	//TODO implement me
	panic("implement me")
}

func (e *Event) GetErrors() []error {
	//TODO implement me
	panic("implement me")
}

// GORMEventStore is a projection that uses GORM to persist events
type GORMEventStore struct {
	handlers        map[string]map[string][]EventHandler
	handlerPanicked bool
	gormDB          *gorm.DB
}

// Dispatch dispatches the event to the handlers
func (e *GORMEventStore) Dispatch(ctx context.Context, logger Log, event *Event, options *EventOptions) []error {
	//mutex helps keep state between routines
	var errors []error
	var wg sync.WaitGroup
	var handlers []EventHandler
	var ok bool
	if globalHandlers := e.handlers[""]; globalHandlers != nil {
		if handlers, ok = globalHandlers[event.Type]; ok {

		}
	}
	if resourceTypeHandlers, ok := e.handlers[event.Meta.ResourceType]; ok {
		if thandlers, ok := resourceTypeHandlers[event.Type]; ok {
			handlers = append(handlers, thandlers...)
		} else {
			if thandlers, ok = resourceTypeHandlers[""]; ok {
				handlers = append(handlers, thandlers...)
			}
		}
	}

	for i := 0; i < len(handlers); i++ {
		handler := handlers[i]
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("handler panicked %s", r)
				}
				wg.Done()
			}()

			err := handler(ctx, logger, event, options)
			if err != nil {
				errors = append(errors, err)
			}

		}()
	}
	wg.Wait()

	return errors
}

// AddSubscriber adds a subscriber to the event dispatcher
func (e *GORMEventStore) AddSubscriber(handler EventHandlerConfig) error {
	if handler.Handler == nil {
		return fmt.Errorf("event handler cannot be nil")
	}
	if e.handlers == nil {
		e.handlers = make(map[string]map[string][]EventHandler)
	}
	if _, ok := e.handlers[handler.ResourceType]; !ok {
		e.handlers[handler.ResourceType] = make(map[string][]EventHandler)
	}
	if _, ok := e.handlers[handler.ResourceType][handler.Type]; !ok {
		e.handlers[handler.ResourceType][handler.Type] = make([]EventHandler, 0)
	}
	e.handlers[handler.ResourceType][handler.Type] = append(e.handlers[handler.ResourceType][handler.Type], handler.Handler)
	return nil
}

func (e *GORMEventStore) GetSubscribers(resourceType string) map[string][]EventHandler {
	if handlers, ok := e.handlers[resourceType]; ok {
		return handlers
	}
	return nil
}

func (e *GORMEventStore) GetByURI(ctxt context.Context, logger Log, uri string) (Resource, error) {
	resource := new(Event)
	result := e.gormDB.Where("id = ?", uri).First(resource)
	if result.Error != nil {
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, result.Error
		} else {
			return nil, nil
		}
	}
	return resource, nil
}

func (e *GORMEventStore) GetByKey(ctxt context.Context, identifiers map[string]interface{}) (Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (e *GORMEventStore) GetList(ctx context.Context, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]Resource, int64, error) {
	//TODO implement me
	panic("implement me")
}

func (e *GORMEventStore) GetByProperties(ctxt context.Context, identifiers map[string]interface{}) ([]Entity, error) {
	//TODO implement me
	panic("implement me")
}

// Persist persists the events to the database
func (e *GORMEventStore) Persist(ctxt context.Context, logger Log, resources []Resource) (errs []error) {
	var events []*Event
	for _, resource := range resources {
		if event, ok := resource.(*Event); ok {
			if event.ID == "" {
				event.ID = ksuid.New().String()
			}
			event.CreatedAt = time.Now()
			event.UpdatedAt = time.Now()
			events = append(events, event)
		} else {
			errs = append(errs, errors.New("resource is not an event"))
		}
	}
	result := e.gormDB.Save(events)
	if result.Error != nil {
		errs = append(errs, result.Error)
	}
	for _, event := range events {
		e.Dispatch(ctxt, logger, event, &EventOptions{
			GORMDB:     e.gormDB,
			HttpClient: NewClient(),
		})
	}
	return errs
}

func (e *GORMEventStore) Remove(ctxt context.Context, logger Log, resources []Resource) []error {
	//TODO implement me
	panic("implement me")
}

func (e *GORMEventStore) GetEventHandlers() []EventHandlerConfig {
	return []EventHandlerConfig{
		{
			ResourceType: "",
			Type:         "create",
			Handler:      e.ResourceUpdateHandler,
		},
		{
			ResourceType: "",
			Type:         "update",
			Handler:      e.ResourceUpdateHandler,
		},
		{
			ResourceType: "",
			Type:         "delete",
			Handler:      e.ResourceDeleteHandler,
		},
	}
}

// ResourceUpdateHandler handles Create Update operations
func (e *GORMEventStore) ResourceUpdateHandler(ctx context.Context, logger Log, event *Event, options *EventOptions) (err error) {
	basicResource := new(BasicResource)
	basicResource.Metadata.ID = event.Meta.ResourceID
	basicResource.Metadata.SequenceNo = event.Meta.SequenceNo
	basicResource.Body = event.Payload
	result := options.GORMDB.Save(basicResource)
	if result.Error != nil {
		return result.Error
	}
	return err
}

// ResourceDeleteHandler handles Delete operations
func (e *GORMEventStore) ResourceDeleteHandler(ctx context.Context, logger Log, event *Event, options *EventOptions) (err error) {
	basicResource := new(BasicResource)
	basicResource.Body = event.Payload
	result := options.GORMDB.Delete(basicResource)
	if result.Error != nil {
		return result.Error
	}
	return err
}

// GetByResourceID gets events by resource id
func (e *GORMEventStore) GetByResourceID(ctxt context.Context, logger Log, resourceID string) (events []*Event, err error) {
	events = make([]*Event, 0)
	result := e.gormDB.Model(&Event{}).Where("resource_id = ?", resourceID).Find(events)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		logger.Errorf("error getting events for resource %s: %v", resourceID, result.Error)
		err = result.Error
		return
	}

	return
}
