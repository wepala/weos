package model

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"time"

	ds "github.com/ompluscator/dynamic-struct"
	"github.com/segmentio/ksuid"
	context2 "github.com/wepala/weos/context"
	"golang.org/x/net/context"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type EventRepositoryGorm struct {
	//DB              *gorm.DB
	//gormDB          *gorm.DB
	eventDispatcher DefaultEventDisptacher
	logger          Log
	unitOfWork      bool
	AccountID       string
	ApplicationID   string
	GroupID         string
	UserID          string
	Container       Container
}

type GormEvent struct {
	gorm.Model
	ID            string
	EntityID      string `gorm:"index"`
	EntityType    string `gorm:"index"`
	Payload       datatypes.JSON
	Type          string `gorm:"index"`
	RootID        string `gorm:"index"`
	ApplicationID string `gorm:"index"`
	AccountID     string `gorm:"index"`
	User          string `gorm:"index"`
	SequenceNo    int64
}

//NewGormEvent converts a domain event to something that is a bit easier for Gorm to work with
func NewGormEvent(event *Event) (GormEvent, error) {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return GormEvent{}, err
	}

	return GormEvent{
		ID:            event.ID,
		EntityID:      event.Meta.EntityID,
		EntityType:    event.Meta.EntityType,
		Payload:       payload,
		Type:          event.Type,
		RootID:        event.Meta.RootID,
		ApplicationID: event.Meta.ApplicationID,
		AccountID:     event.Meta.AccountID,
		User:          event.Meta.User,
		SequenceNo:    event.Meta.SequenceNo,
	}, nil
}

func (e *EventRepositoryGorm) Persist(ctxt context.Context, entity AggregateInterface) error {
	var gormEvents []GormEvent
	entities := entity.GetNewChanges()
	savePointID := "s" + ksuid.New().String() //NOTE the save point can't start with a number
	e.logger.Infof("persisting %d events with save point %s", len(entities), savePointID)
	if e.unitOfWork {
		e.DB().SavePoint(savePointID)
	}

	for _, entity := range entities {
		event := entity.(*Event)
		//let's fill in meta data if it's not already in the object
		if event.Meta.User == "" {
			event.Meta.User = context2.GetUser(ctxt)
		}
		if event.Meta.RootID == "" {
			event.Meta.RootID = context2.GetAccount(ctxt)
		}
		if event.Meta.ApplicationID == "" {
			event.Meta.ApplicationID = context2.GetApplication(ctxt)
		}
		if event.Meta.AccountID == "" {
			event.Meta.AccountID = context2.GetAccount(ctxt)
		}
		if event.Meta.EntityType == "ContentEntity" || event.Meta.EntityType == "" {
			if ce, ok := entity.(*ContentEntity); ok {
				event.Meta.EntityType = ce.Name
			}
		}
		if !event.IsValid() {
			for _, terr := range event.GetErrors() {
				e.logger.Errorf("error encountered persisting entity '%s', '%s'", event.Meta.EntityID, terr)
			}
			if e.unitOfWork {
				e.logger.Debugf("rolling back saving events to %s", savePointID)
				e.DB().RollbackTo(savePointID)
			}

			return event.GetErrors()[0]
		}

		gormEvent, err := NewGormEvent(event)
		if err != nil {
			return err
		}
		gormEvents = append(gormEvents, gormEvent)
	}
	db := e.DB().CreateInBatches(gormEvents, 2000)
	if db.Error != nil {
		return db.Error
	}

	//call persist on the aggregate root to clear the new changes array
	entity.Persist()

	var errs []error
	for _, entity := range entities {
		errors := e.eventDispatcher.Dispatch(ctxt, *entity.(*Event))
		if len(errors) > 0 {
			errs = append(errs, errors...)
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}

	return nil
}

//GetByAggregate get events for a root aggregate
func (e *EventRepositoryGorm) GetByAggregate(ID string) ([]*Event, error) {
	var events []GormEvent
	result := e.DB().Order("sequence_no asc").Where("root_id = ?", ID).Find(&events)
	if result.Error != nil {
		return nil, result.Error
	}

	var tevents []*Event

	for _, event := range events {
		tevents = append(tevents, &Event{
			ID:      event.ID,
			Type:    event.Type,
			Payload: json.RawMessage(event.Payload),
			Meta: EventMeta{
				EntityID:      event.EntityID,
				EntityType:    event.EntityType,
				RootID:        event.RootID,
				ApplicationID: event.ApplicationID,
				User:          event.User,
				SequenceNo:    event.SequenceNo,
			},
			Version: 0,
		})
	}
	return tevents, nil

}

//GetByAggregateAndType returns events given the entity id and the entity type.
//Deprecated: 08/12/2021 This was in theory returning events by entity (not necessarily root aggregate). Upon introducing the RootID
//events should now be retrieved by root id,entity type and entity id. Use GetByEntityAndAggregate instead
func (e *EventRepositoryGorm) GetByAggregateAndType(ID string, entityType string) ([]*Event, error) {
	var events []GormEvent
	result := e.DB().Order("sequence_no asc").Where("entity_id = ? AND entity_type = ?", ID, entityType).Find(&events)
	if result.Error != nil {
		return nil, result.Error
	}

	var tevents []*Event

	for _, event := range events {
		tevents = append(tevents, &Event{
			ID:      event.ID,
			Type:    event.Type,
			Payload: json.RawMessage(event.Payload),
			Meta: EventMeta{
				EntityID:      event.EntityID,
				EntityType:    event.EntityType,
				RootID:        event.RootID,
				ApplicationID: event.ApplicationID,
				User:          event.User,
				SequenceNo:    event.SequenceNo,
			},
			Version: 0,
		})
	}
	return tevents, nil
}

func (e *EventRepositoryGorm) GetByEntityAndAggregate(EntityID string, Type string, RootID string) ([]*Event, error) {
	var events []GormEvent
	result := e.DB().Order("sequence_no asc").Where("entity_id = ? AND entity_type = ? AND root_id = ?", EntityID, Type, RootID).Find(&events)
	if result.Error != nil {
		return nil, result.Error
	}

	var tevents []*Event

	for _, event := range events {
		tevents = append(tevents, &Event{
			ID:      event.ID,
			Type:    event.Type,
			Payload: json.RawMessage(event.Payload),
			Meta: EventMeta{
				EntityID:      event.EntityID,
				EntityType:    event.EntityType,
				RootID:        event.RootID,
				ApplicationID: event.ApplicationID,
				User:          event.User,
				SequenceNo:    event.SequenceNo,
			},
			Version: 0,
		})
	}
	return tevents, nil
}

//GetAggregateSequenceNumber gets the latest sequence number for the aggregate entity
func (e *EventRepositoryGorm) GetAggregateSequenceNumber(ID string) (int64, error) {
	var event GormEvent
	result := e.DB().Order("sequence_no desc").Where("root_id = ?", ID).Find(&event)
	if result.Error != nil {
		return 0, result.Error
	}
	return event.SequenceNo, nil
}

func (e *EventRepositoryGorm) GetByAggregateAndSequenceRange(ID string, start int64, end int64) ([]*Event, error) {
	var events []GormEvent
	result := e.DB().Order("sequence_no asc").Where("entity_id = ? AND sequence_no >=? AND sequence_no <= ?", ID, start, end).Find(&events)
	if result.Error != nil {
		return nil, result.Error
	}
	var tevents []*Event

	for _, event := range events {
		tevents = append(tevents, &Event{
			ID:      event.ID,
			Type:    event.Type,
			Payload: json.RawMessage(event.Payload),
			Meta: EventMeta{
				EntityID:      event.EntityID,
				EntityType:    event.EntityType,
				RootID:        event.RootID,
				ApplicationID: event.ApplicationID,
				User:          event.User,
				SequenceNo:    event.SequenceNo,
			},
			Version: 0,
		})
	}
	return tevents, nil
}

//AddSubscriber Allows you to add a handler that is triggered when events are dispatched
func (e *EventRepositoryGorm) AddSubscriber(handler EventHandler) {
	e.eventDispatcher.AddSubscriber(handler)
}

//GetSubscribers Get the current list of event subscribers
func (e *EventRepositoryGorm) GetSubscribers() ([]EventHandler, error) {
	return e.eventDispatcher.GetSubscribers(), nil
}

func (e *EventRepositoryGorm) Migrate(ctx context.Context) error {
	event, err := NewGormEvent(&Event{})
	if err != nil {
		return err
	}
	err = e.DB().AutoMigrate(&event)
	if err != nil {
		return err
	}

	return nil
}

func (e *EventRepositoryGorm) Flush() error {
	err := e.DB().Commit().Error
	e.DB().Begin()
	return err
}

func (e *EventRepositoryGorm) Remove(entities []Entity) error {

	savePointID := "s" + ksuid.New().String() //NOTE the save point can't start with a number
	e.logger.Infof("persisting %d events with save point %s", len(entities), savePointID)
	e.DB().SavePoint(savePointID)
	for _, event := range entities {
		gormEvent, err := NewGormEvent(event.(*Event))
		if err != nil {
			return err
		}
		db := e.DB().Delete(gormEvent)
		if db.Error != nil {
			e.DB().RollbackTo(savePointID)
			return db.Error
		}
	}

	return nil
}

//Content may not be applicable to this func since there would be an instance of it being called at server.go run. Therefore we won't have a "proper" content which would contain the EntityFactory
func (e *EventRepositoryGorm) ReplayEvents(ctxt context.Context, date time.Time, entityFactories map[string]EntityFactory, projection Projection, schema *openapi3.Swagger) (int, int, int, []error) {
	var errors []error
	var errArray []error

	schemas := make(map[string]ds.Builder)

	for _, value := range entityFactories {
		schemas[value.Name()] = value.Builder(context.Background())
	}

	err := projection.Migrate(ctxt, schema)
	if err != nil {
		e.logger.Errorf("error migrating tables: %s", err)
	}

	var events []GormEvent

	if date.IsZero() {
		result := e.DB().Table("gorm_events").Order("created_at asc").Find(&events)
		if result.Error != nil {
			e.logger.Errorf("got error pulling events '%s'", result.Error)
			errors = append(errors, result.Error)
			return 0, 0, 0, errors
		}
	} else {
		result := e.DB().Table("gorm_events").Where("created_at =  ?", date).Find(&events)
		if result.Error != nil {
			e.logger.Errorf("got error pulling events '%s'", result.Error)
			errors = append(errors, result.Error)
			return 0, 0, 0, errors
		}
	}

	var tEvents []*Event

	for _, event := range events {
		tEvents = append(tEvents, &Event{
			ID:      event.ID,
			Type:    event.Type,
			Payload: json.RawMessage(event.Payload),
			Meta: EventMeta{
				EntityID:      event.EntityID,
				EntityType:    event.EntityType,
				RootID:        event.RootID,
				ApplicationID: event.ApplicationID,
				User:          event.User,
				SequenceNo:    event.SequenceNo,
			},
			Version: 0,
		})
	}

	totalEvents := len(tEvents)
	successfulEvents := 0
	failedEvents := 0

	for _, event := range tEvents {

		newContext := context.WithValue(ctxt, context2.ENTITY_FACTORY, entityFactories[event.Meta.EntityType])
		errArray = e.eventDispatcher.Dispatch(newContext, *event)
		if len(errArray) == 0 {
			successfulEvents++
		} else {
			errors = append(errors, errArray...)
			failedEvents++
		}

	}
	return totalEvents, successfulEvents, failedEvents, errors
}

func (e *EventRepositoryGorm) DB() *gorm.DB {
	t, _ := e.Container.GetGormDBConnection("default")
	return t
}

func NewBasicEventRepository(gormDB *gorm.DB, logger Log, useUnitOfWork bool, accountID string, applicationID string, api Container) (EventRepository, error) {
	if useUnitOfWork {
		//transaction := gormDB.Begin()
		return &EventRepositoryGorm{logger: logger, unitOfWork: useUnitOfWork, AccountID: accountID, ApplicationID: applicationID, Container: api}, nil
	}
	return &EventRepositoryGorm{logger: logger, AccountID: accountID, ApplicationID: applicationID, Container: api}, nil
}
