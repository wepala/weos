package model

import (
	"fmt"
	"golang.org/x/net/context"
)

type Receiver struct {
	service       Service
	domainService *DomainService
}

//CreateHandler is used to add entities to the repository.
func CreateHandler(ctx context.Context, command *Command, container Container, repository EntityRepository, logger Log) (interface{}, error) {
	var err error
	var entity *ContentEntity
	entity, err = repository.CreateEntityWithValues(ctx, command.Payload)
	if err != nil {
		logger.Errorf("error creating entity: %s", err)
		return nil, err
	}
	if entity.IsValid() {
		//save entity if the projection is a gorm projection, we can use the persist method
		err = repository.Persist([]Entity{entity})
		if err != nil {
			logger.Errorf("error persisting entity: %s", err)
			return nil, err
		}
		eventStore, err := container.GetEventStore("Default")
		if err != nil {
			logger.Errorf("error getting event store: %s", err)
			return nil, err
		}
		return entity, eventStore.Persist(ctx, entity)

	} else {
		return nil, entity.GetErrors()[0]
	}
}

//UpdateHandler is used for a single payload. It takes in the command and context which is used to dispatch and updated the specified entity.
func UpdateHandler(ctx context.Context, command *Command, container Container, repository EntityRepository, logger Log) (interface{}, error) {
	var err error
	var entity *ContentEntity
	if logger == nil {
		logger, err = container.GetLog("Default")
		if err != nil {
			return nil, fmt.Errorf("no logger set")
		}
	}
	//get the entity from the repository by id if the entity id is in the command
	if command.Metadata.EntityID != "" {
		entity, err = repository.GetContentEntity(ctx, repository, command.Metadata.EntityID)
		//TODO check if the sequence numbers is the same as in the command otherwise throw error
	}
	//TODO if entity is empty then let's get the entity by key
	_, err = entity.Update(ctx, command.Payload)
	if err != nil {
		logger.Errorf("error updating entity: %s", err)
		return nil, err
	}
	if entity.IsValid() {
		err = repository.Persist([]Entity{entity})
		if err != nil {
			logger.Errorf("error persisting entity: %s", err)
			return nil, err
		}
		var eventStore EventRepository
		eventStore, err = container.GetEventStore("Default")
		if err != nil {
			logger.Errorf("error getting event store: %s", err)
			return nil, err
		}
		err = eventStore.Persist(ctx, entity)
	}
	return entity, err
}

//DeleteHandler is used for a single entity. It takes in the command and context which is used to dispatch and delete the specified entity.
func DeleteHandler(ctx context.Context, command *Command, container Container, repository EntityRepository, logger Log) (interface{}, error) {
	var err error
	var entity *ContentEntity
	if logger == nil {
		logger, err = container.GetLog("Default")
		if err != nil {
			return nil, fmt.Errorf("no logger set")
		}
	}

	//get the entity from the repository by id if the entity id is in the command
	if command.Metadata.EntityID != "" {
		entity, err = repository.GetContentEntity(ctx, repository, command.Metadata.EntityID)
		//TODO check if the sequence numbers is the same as in the command otherwise throw error
	}
	//TODO if entity is empty then let's get the entity by key
	_, err = entity.Delete(command.Payload)
	if err != nil {
		logger.Errorf("error updating entity: %s", err)
		return nil, err
	}
	var eventStore EventRepository
	eventStore, err = container.GetEventStore("Default")
	if err != nil {
		logger.Errorf("error getting event store: %s", err)
		return nil, err
	}
	err = eventStore.Persist(ctx, entity)
	return entity, err
}
