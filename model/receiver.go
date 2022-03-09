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
		logger.Debug(err)
		return err
	}
	//Generate id for entity
	payload, err = GenerateID(payload, entityFactory)
	if err != nil {
		err = NewDomainError("unexpected error generating id for entity", command.Metadata.EntityType, "", err)
		logger.Debug(err)
		return err
	}
	//use the entity id that was passed with the command
	newEntity.ID = command.Metadata.EntityID
	//add create event
	event := NewEntityEvent("create", newEntity, newEntity.ID, payload)
	//ddd userid to event
	event.Meta.User = command.Metadata.UserID
	newEntity.NewChange(event)
	err = newEntity.ApplyEvents([]*Event{event})
	if err != nil {
		err = NewDomainError("unexpected error creating entity: "+err.Error(), command.Metadata.EntityType, "", err)
		logger.Debug(err.Error())
		return err
	}

	domainService := NewDomainService(ctx, eventStore, projection, logger)
	err = domainService.ValidateUnique(ctx, newEntity)
	if err != nil {
		return err
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

//CreateBatchHandler is used for an array of payloads. It takes in the command and context which is used to dispatch and the persist the incoming request.
func CreateBatchHandler(ctx context.Context, command *Command, eventStore EventRepository, projection Projection, logger Log) error {
	domainService := NewDomainService(ctx, eventStore, projection, logger)
	entities, err := domainService.CreateBatch(ctx, command.Payload, command.Metadata.EntityType)
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
func UpdateHandler(ctx context.Context, command *Command, eventStore EventRepository, projection Projection, logger Log) error {
	if logger == nil {
		return fmt.Errorf("no logger set")
	}
	entityFactory := GetEntityFactory(ctx)
	if entityFactory == nil {
		err := errors.New("no entity factory found")
		logger.Error(err)
		return err
	}
	//initialize any services
	domainService := NewDomainService(context.Background(), eventStore, projection, logger)
	updatedEntity, err := domainService.Update(ctx, command.Payload, command.Metadata.EntityType)
	if err != nil {
		return err
	}
	err = eventStore.Persist(ctx, updatedEntity)
	if err != nil {
		return err
	}
	return nil
}

//DeleteHandler is used for a single entity. It takes in the command and context which is used to dispatch and delete the specified entity.
func DeleteHandler(ctx context.Context, command *Command, eventStore EventRepository, projection Projection, logger Log) error {
	if logger == nil {
		return fmt.Errorf("no logger set")
	}
	entityFactory := GetEntityFactory(ctx)
	if entityFactory == nil {
		err := errors.New("no entity factory found")
		logger.Error(err)
		return err
	}

	//initialize any services
	domainService := NewDomainService(ctx, eventStore, projection, logger)
	deletedEntity, err := domainService.Delete(ctx, command.Metadata.EntityID, command.Metadata.EntityType)
	if err != nil {
		return err
	}

	err = eventStore.Persist(ctx, deletedEntity)
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
	service.Dispatcher().AddSubscriber(CreateBatch(context.Background(), payload, ""), CreateBatchHandler)
	service.Dispatcher().AddSubscriber(Update(context.Background(), payload, ""), UpdateHandler)
	service.Dispatcher().AddSubscriber(Delete(context.Background(), "", ""), DeleteHandler)
	//initialize any services
	receiver.domainService = NewDomainService(context.Background(), service.EventRepository(), nil, nil)

	for _, projection := range service.Projections() {
		if projections, ok := projection.(Projection); ok {
			receiver.domainService = NewDomainService(context.Background(), service.EventRepository(), projections, nil)
		}
	}

	if receiver.domainService == nil {
		return NewError("no projection provided", nil)
	}
	return nil
}
