package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/context"
)

type Receiver struct {
	service       Service
	domainService *DomainService
}

//CreateHandler is used for a single payload. It takes in the command and context which is used to dispatch and the persist the incoming request.
func CreateHandler(ctx context.Context, command *Command, eventStore EventRepository, projection Projection, logger Log) error {
	if logger == nil {
		return fmt.Errorf("no logger set")
	}

	payload, err := AddIDToPayload(command.Payload, command.Metadata.EntityID)
	if err != nil {
		return err
	}

	entityFactory := GetEntityFactory(ctx)
	if entityFactory == nil {
		err = errors.New("no entity factory found")
		logger.Error(err)
		return err
	}
	newEntity, err := entityFactory.NewEntity(ctx)
	if err != nil {
		err = NewDomainError("unexpected error creating entity", command.Metadata.EntityType, "", err)
		logger.Error(err)
		return err
	}
	//use the entity id that was passed with the command
	newEntity.ID = command.Metadata.EntityID
	//add create event
	event := NewEntityEvent("create", newEntity, newEntity.ID, payload)
	newEntity.NewChange(event)
	err = newEntity.ApplyEvents([]*Event{event})
	if err != nil {
		return NewDomainError(err.Error(), command.Metadata.EntityType, "", err)
	}

	if ok := newEntity.IsValid(); !ok {
		errors := newEntity.GetErrors()
		if len(errors) != 0 {
			return NewDomainError(errors[0].Error(), command.Metadata.EntityType, newEntity.ID, errors[0])
		}
	}
	err = eventStore.Persist(ctx, newEntity)
	if err != nil {
		return err
	}
	return nil
}

//CreateBatch is used for an array of payloads. It takes in the command and context which is used to dispatch and the persist the incoming request.
func (r *Receiver) CreateBatch(ctx context.Context, command *Command, eventStore EventRepository, projection Projection, logger Log) error {
	entities, err := r.domainService.CreateBatch(ctx, command.Payload, command.Metadata.EntityType)
	if err != nil {
		return err
	}
	for _, entity := range entities {
		err = eventStore.Persist(ctx, entity)
		if err != nil {
			return err
		}
	}
	return nil
}

//Update is used for a single payload. It takes in the command and context which is used to dispatch and updated the specified entity.
func (r *Receiver) Update(ctx context.Context, command *Command, eventStore EventRepository, projection Projection, logger Log) error {

	updatedEntity, err := r.domainService.Update(ctx, command.Payload, command.Metadata.EntityType)
	if err != nil {
		return err
	}
	err = eventStore.Persist(ctx, updatedEntity)
	if err != nil {
		return err
	}
	return nil
}

//Deprecated: 01/30/2022 These are setup in the api initializer
//Initialize sets up the command handlers
func Initialize(service Service) error {
	var payload json.RawMessage
	//Initialize receiver
	receiver := &Receiver{service: service}
	//add command handlers to the application's command dispatcher
	service.Dispatcher().AddSubscriber(Create(context.Background(), payload, "", ""), CreateHandler)
	service.Dispatcher().AddSubscriber(CreateBatch(context.Background(), payload, ""), receiver.CreateBatch)
	service.Dispatcher().AddSubscriber(Update(context.Background(), payload, ""), receiver.Update)
	//initialize any services
	receiver.domainService = NewDomainService(context.Background(), service.EventRepository(), nil)

	for _, projection := range service.Projections() {
		if projections, ok := projection.(Projection); ok {
			receiver.domainService = NewDomainService(context.Background(), service.EventRepository(), projections)
		}
	}

	if receiver.domainService == nil {
		return NewError("no projection provided", nil)
	}
	return nil
}
