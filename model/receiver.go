package model

import (
	"encoding/json"
	"golang.org/x/net/context"
)

type Receiver struct {
	service       Service
	domainService *DomainService
}

//Create is used for a single payload. It takes in the command and context which is used to dispatch and the persist the incoming request.
func (r *Receiver) Create(ctx context.Context, command *Command) error {
	payload, err := AddIDToPayload(command.Payload, command.Metadata.EntityID)
	if err != nil {
		return err
	}

	entity, err := r.domainService.Create(ctx, payload, command.Metadata.EntityType)
	if err != nil {
		return err
	}
	err = r.service.EventRepository().Persist(ctx, entity)
	if err != nil {
		return err
	}
	return nil
}

//CreateBatch is used for an array of payloads. It takes in the command and context which is used to dispatch and the persist the incoming request.
func (r *Receiver) CreateBatch(ctx context.Context, command *Command) error {
	entities, err := r.domainService.CreateBatch(ctx, command.Payload, command.Metadata.EntityType)
	if err != nil {
		return err
	}
	for _, entity := range entities {
		err = r.service.EventRepository().Persist(ctx, entity)
		if err != nil {
			return err
		}
	}
	return nil
}

//Update is used for a single payload. It takes in the command and context which is used to dispatch and updated the specified entity.
func (r *Receiver) Update(ctx context.Context, command *Command) error {
	payload, err := AddIDToPayload(command.Payload, command.Metadata.EntityID)
	if err != nil {
		return err
	}

	updatedEntity, err := r.domainService.Update(ctx, payload, command.Metadata.EntityType)
	if err != nil {
		return err
	}
	err = r.service.EventRepository().Persist(ctx, updatedEntity)
	if err != nil {
		return err
	}
	return nil
}

//Initialize sets up the command handlers
func Initialize(service Service) error {
	var payload json.RawMessage
	//Initialize receiver
	receiver := &Receiver{service: service}
	//add command handlers to the application's command dispatcher
	service.Dispatcher().AddSubscriber(Create(context.Background(), payload, "", ""), receiver.Create)
	service.Dispatcher().AddSubscriber(CreateBatch(context.Background(), payload, ""), receiver.CreateBatch)
	service.Dispatcher().AddSubscriber(Create(context.Background(), payload, "", ""), receiver.Update)
	//initialize any services
	receiver.domainService = NewDomainService(context.Background(), service.EventRepository())

	if receiver.domainService == nil {
		return NewError("no projection provided", nil)
	}
	return nil
}
