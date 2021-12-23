package model

import (
	"encoding/json"
	"golang.org/x/net/context"
)

type Receiver struct {
	service       Service
	domainService *DomainService
}

func (r *Receiver) Create(ctx context.Context, command *Command) error {

	//entity, err := r.domainService.Create(ctx, command.Payload, command.Metadata.EntityType)
	//if err != nil {
	//	return err
	//}
	//err = r.service.EventRepository().Persist(ctx, entity)
	//if err != nil {
	//	return err
	//}
	return nil
}

func (r *Receiver) CreateBatch(ctx context.Context, command *Command) error {

	return nil
}

//Initialize sets up the command handlers
func Initialize(service Service) error {
	var payload json.RawMessage
	//Initialize receiver
	receiver := &Receiver{service: service}
	//add command handlers to the application's command dispatcher
	service.Dispatcher().AddSubscriber(Create(context.Background(), payload, ""), receiver.Create)
	service.Dispatcher().AddSubscriber(CreateBatch(context.Background(), payload, ""), receiver.CreateBatch)
	return nil
}
