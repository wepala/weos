package model

import (
	"encoding/json"
	weosContext "github.com/wepala/weos-service/context"
	"golang.org/x/net/context"
)

type DomainService struct {
	Repository
	eventRepository EventRepository
}

//Create is used for a single payload. It creates a new entity via the FromSchemaWithValue func and then returns the entity
func (s *DomainService) Create(ctx context.Context, payload json.RawMessage, entityType string) (*ContentEntity, error) {

	contentType := weosContext.GetContentType(ctx)
	newEntity, err := new(ContentEntity).FromSchemaWithValues(ctx, contentType.Schema, payload)
	if err != nil {
		return nil, NewDomainError("unexpected error creating entity", entityType, "", err)
	}
	if ok := newEntity.IsValid(); !ok {
		errors := newEntity.GetErrors()
		if len(errors) != 0 {
			return nil, NewDomainError(errors[0].Error(), entityType, newEntity.ID, errors[0])
		}
	}
	return newEntity, nil
}

//CreateBatch is used for an array of payloads. It uses a for loop to create new entities and append it to an array.
func (s *DomainService) CreateBatch(ctx context.Context, payload json.RawMessage, entityType string) ([]*ContentEntity, error) {
	var titems []interface{}
	err := json.Unmarshal(payload, &titems)
	if err != nil {
		return nil, err
	}
	newEntityArr := []*ContentEntity{}
	contentType := weosContext.GetContentType(ctx)
	for _, titem := range titems {
		tpayload, err := json.Marshal(titem)
		if err != nil {
			return nil, err
		}
		entity, err := new(ContentEntity).FromSchemaWithValues(ctx, contentType.Schema, tpayload)
		if err != nil {
			return nil, err
		}
		if ok := entity.IsValid(); !ok {
			return nil, NewDomainError("unexpected error entity is invalid", entityType, entity.ID, nil)
		}
		newEntityArr = append(newEntityArr, entity)
	}

	return newEntityArr, nil

}

//Update is used for a single payload. It gets an existing entity and updates it with the new payload
//TODO Add weosID/EntityID to cmd (when 1130 -> dev)
func (s *DomainService) Update(ctx context.Context, payload json.RawMessage, entityType string) (*ContentEntity, error) {
	//TODO Check weosID if blank
	//TODO call getEntity with id
	//TODO check if entity is nil
	//TODO call entity update
	//TODO Return entity
	return new(ContentEntity), nil
}

func NewDomainService(ctx context.Context, eventRepository EventRepository) *DomainService {
	return &DomainService{
		eventRepository: eventRepository,
	}
}
