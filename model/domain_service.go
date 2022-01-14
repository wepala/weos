package model

import (
	"encoding/json"
	"strconv"

	weosContext "github.com/wepala/weos-service/context"
	"golang.org/x/net/context"
)

type DomainService struct {
	Projection
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
func (s *DomainService) Update(ctx context.Context, payload json.RawMessage, entityType string) (*ContentEntity, error) {
	var existingEntity *ContentEntity
	var updatedEntity *ContentEntity
	var identifier map[string]interface{}
	var weosID string
	contentType := weosContext.GetContentType(ctx)

	//Fetch the weosID from the payload
	weosID, err := GetIDfromPayload(payload)
	if err != nil {
		return nil, NewDomainError("unexpected error unmarshalling payload to get weosID", entityType, "", err)
	}

	//If there is a weosID present use this
	if weosID != "" {
		seqNo, err := GetSeqfromPayload(payload)
		if err != nil {
			return nil, NewDomainError("unexpected error unmarshalling payload to get sequence number", entityType, "", err)
		}

		if seqNo == "" {
			return nil, NewDomainError("no sequence number provided", entityType, "", nil)
		}

		existingEntity, err := s.GetContentEntity(ctx, weosID)
		if err != nil {
			return nil, NewDomainError("unexpected error fetching existing entity", entityType, weosID, err)
		}

		entitySeqNo := strconv.Itoa(int(existingEntity.SequenceNo))

		if seqNo != entitySeqNo {
			return nil, NewDomainError("error updating entity. This is a stale item", entityType, weosID, nil)
		}

		updatedEntity, err = existingEntity.Update(payload)
		if err != nil {
			return nil, NewDomainError("unexpected error updating existingEntity", entityType, weosID, err)
		}

		//If there is no weosID, use the id passed from the param
	} else if weosID == "" {
		paramID := ctx.Value("id")

		if paramID == "" {
			return nil, NewDomainError("no ID provided", entityType, "", nil)
		}

		identifier = map[string]interface{}{"id": paramID}
		entityInterface, err := s.GetByKey(ctx, contentType, identifier)

		data, err := json.Marshal(entityInterface)
		if err != nil {
			return nil, NewDomainError("unexpected error marshalling existingEntity interface", entityType, paramID.(string), err)
		}

		err = json.Unmarshal(data, &existingEntity)
		if err != nil {
			return nil, NewDomainError("unexpected error unmarshalling existingEntity", entityType, paramID.(string), err)
		}

		updatedEntity, err = existingEntity.Update(payload)
		if err != nil {
			return nil, NewDomainError("unexpected error updating existingEntity", entityType, paramID.(string), err)
		}

	}
	return updatedEntity, nil
}

func NewDomainService(ctx context.Context, eventRepository EventRepository, projections Projection) *DomainService {
	return &DomainService{
		eventRepository: eventRepository,
		Projection:      projections,
	}
}
