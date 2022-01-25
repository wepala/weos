package model

import (
	"encoding/json"
	"fmt"

	ds "github.com/ompluscator/dynamic-struct"
	weosContext "github.com/wepala/weos/context"
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
	var updatedEntity *ContentEntity
	existingEntity := &ContentEntity{}
	var weosID string
	contentType := weosContext.GetContentType(ctx)

	//Fetch the weosID from the payload
	weosID, err := GetIDfromPayload(payload)
	if err != nil {
		return nil, err
	}
	if weosID == "" {
		weosID, _ = ctx.Value(weosContext.WEOS_ID).(string)
	}

	var primaryKeys []string
	identifiers := map[string]interface{}{}

	if contentType.Schema.Extensions["x-identifier"] != nil {
		identifiersFromSchema := contentType.Schema.Extensions["x-identifier"].(json.RawMessage)
		json.Unmarshal(identifiersFromSchema, &primaryKeys)
	}

	var tempPayload map[string]interface{}
	err = json.Unmarshal(payload, &tempPayload)
	if err != nil {
		return nil, err
	}

	if len(primaryKeys) == 0 {
		primaryKeys = append(primaryKeys, "id")
	}

	for _, pk := range primaryKeys {
		ctxtIdentifier := ctx.Value(pk)

		if weosID == "" {
			if ctxtIdentifier == nil {
				return nil, NewDomainError("invalid: no value provided for primary key", entityType, "", nil)
			}
		}

		identifiers[pk] = ctxtIdentifier
		tempPayload[pk] = identifiers[pk]

	}

	newPayload, err := json.Marshal(tempPayload)
	if err != nil {
		return nil, err
	}

	//If there is a weosID present use this
	if weosID != "" {
		seqNo := -1
		if seq, ok := ctx.Value(weosContext.SEQUENCE_NO).(int); ok {
			seqNo = seq
		}

		existingEntity, err := s.GetContentEntity(ctx, weosID)
		if err != nil {
			return nil, NewDomainError("invalid: unexpected error fetching existing entity", entityType, weosID, err)
		}

		if seqNo != -1 && existingEntity.SequenceNo != int64(seqNo) {
			return nil, NewDomainError("error updating entity. This is a stale item", entityType, weosID, nil)
		}

		reader := ds.NewReader(existingEntity.Property)
		for _, f := range reader.GetAllFields() {
			fmt.Print(f)
			reader.GetValue()
		}

		existingEntityPayload, err := json.Marshal(existingEntity.Property)
		if err != nil {
			return nil, err
		}

		var tempExistingPayload map[string]interface{}

		err = json.Unmarshal(existingEntityPayload, &tempExistingPayload)
		if err != nil {
			return nil, err
		}

		for _, pk := range primaryKeys {
			if fmt.Sprint(tempExistingPayload[pk]) != fmt.Sprint(tempPayload[pk]) {
				return nil, NewDomainError("invalid: error updating entity. Primary keys cannot be updated.", entityType, weosID, nil)
			}
		}

		updatedEntity, err = existingEntity.Update(ctx, existingEntityPayload, newPayload)
		if err != nil {
			return nil, err
		}

		if ok := updatedEntity.IsValid(); !ok {
			return nil, NewDomainError("unexpected error entity is invalid", entityType, updatedEntity.ID, nil)
		}

		//If there is no weosID, use the id passed from the param
	} else if weosID == "" {

		entityInterface, err := s.GetByKey(ctx, *contentType, identifiers)
		if err != nil {
			return nil, NewDomainError("invalid: unexpected error fetching existing entity", entityType, "", err)
		}

		data, err := json.Marshal(entityInterface)
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal(data, &existingEntity)
		if err != nil {
			return nil, err
		}

		updatedEntity, err = existingEntity.Update(ctx, data, newPayload)
		if err != nil {
			return nil, err
		}

		if ok := updatedEntity.IsValid(); !ok {
			return nil, NewDomainError("unexpected error entity is invalid", entityType, updatedEntity.ID, nil)
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
