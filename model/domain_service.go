package model

import (
	"encoding/json"
	"errors"
	"fmt"
	weosContext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
)

type DomainService struct {
	Projection
	Repository
	eventRepository EventRepository
	logger          Log
}

//Create is used for a single payload. It creates a new entity via the FromSchemaWithValue func and then returns the entity
func (s *DomainService) Create(ctx context.Context, payload json.RawMessage, entityType string) (*ContentEntity, error) {

	contentType := weosContext.GetContentType(ctx)
	newEntity, err := new(ContentEntity).FromSchemaWithValues(ctx, contentType.Schema, payload)
	if err != nil {
		return nil, NewDomainError("unexpected error creating entity", entityType, "", err)
	}
	err = s.ValidateUnique(ctx, newEntity)
	if err != nil {
		return nil, err
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
	entityFactory := GetEntityFactory(ctx)
	if entityFactory == nil {
		err = errors.New("no entity factory found")
		s.logger.Error(err)
		return nil, err
	}
	for _, titem := range titems {
		if err != nil {
			return nil, err
		}
		//get the bytes for a single item
		itemPayload, err := json.Marshal(titem)
		if err != nil {
			return nil, err
		}
		entity, err := entityFactory.CreateEntityWithValues(ctx, itemPayload)
		if err != nil {
			return nil, err
		}
		err = s.ValidateUnique(ctx, entity)
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
	var weosID string
	entityFactory := GetEntityFactory(ctx)
	if entityFactory == nil {
		return nil, errors.New("entity factory must be set")
	}
	existingEntity, err := entityFactory.NewEntity(ctx)
	if err != nil {
		s.logger.Errorf("error creating new entity '%s'", err)
	}

	//Fetch the weosID from the payload
	weosID, err = GetIDfromPayload(payload)
	if err != nil {
		return nil, err
	}
	if weosID == "" {
		weosID, _ = ctx.Value(weosContext.WEOS_ID).(string)
	}

	//get the properties that make up the identifier from the schema
	var primaryKeys []string
	identifiers := map[string]interface{}{}

	if entityFactory.Schema().Extensions["x-identifier"] != nil {
		identifiersFromSchema := entityFactory.Schema().Extensions["x-identifier"].(json.RawMessage)
		json.Unmarshal(identifiersFromSchema, &primaryKeys)
	}

	//if there is no identifier specified in the schema then use "id"
	if len(primaryKeys) == 0 {
		primaryKeys = append(primaryKeys, "id")
	}

	//for each identifier part pull the value from the context and store in a map
	for _, pk := range primaryKeys {
		ctxtIdentifier := ctx.Value(pk)
		identifiers[pk] = ctxtIdentifier
	}

	//If there is a weosID present use this
	if weosID != "" {
		seqNo := -1
		if seq, ok := ctx.Value(weosContext.SEQUENCE_NO).(int); ok {
			seqNo = seq
		}

		existingEntity, err = s.GetContentEntity(ctx, entityFactory, weosID)
		if err != nil {
			return nil, NewDomainError("invalid: unexpected error fetching existing entity", entityType, weosID, err)
		}

		if existingEntity == nil {
			return nil, NewDomainError("entity not found", entityType, weosID, nil)
		}

		if seqNo != -1 && existingEntity.SequenceNo != int64(seqNo) {
			return nil, NewDomainError("error updating entity. This is a stale item", entityType, weosID, nil)
		}
		//If there is no weosID, use the id passed from the param
	} else if weosID == "" {
		seqNo := -1
		if seq, ok := ctx.Value(weosContext.SEQUENCE_NO).(int); ok {
			seqNo = seq
		}
		//temporary fiv

		existingEntity, err = s.GetByKey(ctx, entityFactory, identifiers)
		if err != nil {
			s.logger.Errorf("error updating entity", err)
			return nil, NewDomainError("invalid: unexpected error fetching existing entity", entityType, "", err)
		}

		if seqNo != -1 && existingEntity.SequenceNo != int64(seqNo) {
			return nil, NewDomainError("error updating entity. This is a stale item", entityType, weosID, nil)
		}

	}

	//update default time update values based on routes
	operation, ok := ctx.Value(weosContext.OPERATION_ID).(string)
	if ok {
		err = existingEntity.UpdateTime(operation)
	}
	if err != nil {
		return nil, err
	}

	//update the entity
	updatedEntity, err = existingEntity.Update(ctx, payload)
	if err != nil {
		return nil, err
	}

	err = s.ValidateUnique(ctx, updatedEntity)
	if err != nil {
		return nil, err
	}
	if ok := updatedEntity.IsValid(); !ok {
		return nil, NewDomainError("unexpected error entity is invalid", entityType, updatedEntity.ID, nil)
	}

	return updatedEntity, nil
}

//Delete is used for a single entity. It takes in the command and context which is used to dispatch and delete the specified entity.
func (s *DomainService) Delete(ctx context.Context, entityID string, entityType string) (*ContentEntity, error) {
	var existingEntity *ContentEntity
	var deletedEntity *ContentEntity
	var err error

	//try to get the entity id from the context
	if entityID == "" {
		entityID, _ = ctx.Value(weosContext.WEOS_ID).(string)
	}

	entityFactory := GetEntityFactory(ctx)
	if entityFactory == nil {
		return nil, errors.New("entity factory must be set")
	}

	var primaryKeys []string
	identifiers := map[string]interface{}{}

	//check the schema for the name of the properties that make up the identifier
	if entityFactory.Schema().Extensions["x-identifier"] != nil {
		identifiersFromSchema := entityFactory.Schema().Extensions["x-identifier"].(json.RawMessage)
		json.Unmarshal(identifiersFromSchema, &primaryKeys)
	}

	//if there are no primary keys
	if len(primaryKeys) == 0 {
		primaryKeys = append(primaryKeys, "id")
	}

	for _, pk := range primaryKeys {
		ctxtIdentifier := ctx.Value(pk)

		if entityID == "" {
			if ctxtIdentifier == nil {
				return nil, NewDomainError("invalid: no value provided for primary key", entityType, "", nil)
			}
		}

		identifiers[pk] = ctxtIdentifier

	}

	//If there is a weosID present use this
	if entityID != "" {
		seqNo := -1
		if seq, ok := ctx.Value(weosContext.SEQUENCE_NO).(int); ok {
			seqNo = seq
		}

		existingEntity, err = s.GetContentEntity(ctx, entityFactory, entityID)
		if err != nil {
			return nil, NewDomainError("invalid: unexpected error fetching existing entity", entityType, entityID, err)
		}

		if seqNo != -1 && existingEntity.SequenceNo != int64(seqNo) {
			return nil, NewDomainError("error deleting entity. This is a stale item", entityType, entityID, nil)
		}

		existingEntityPayload, err := json.Marshal(existingEntity.payload)
		if err != nil {
			return nil, err
		}

		//update default time update values based on routes
		operation, ok := ctx.Value(weosContext.OPERATION_ID).(string)
		if ok {
			err = existingEntity.UpdateTime(operation)
			if err != nil {
				return nil, err
			}
		}

		deletedEntity, err = existingEntity.Delete(existingEntityPayload)
		if err != nil {
			return nil, err
		}

		if deletedEntity == nil {
			return nil, NewDomainError("error deleting entity.", entityType, entityID, nil)
		}

	} else if entityID == "" {

		entityInterface, err := s.GetByKey(ctx, entityFactory, identifiers)
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

		//update default time update values based on routes
		operation, ok := ctx.Value(weosContext.OPERATION_ID).(string)
		if ok {
			err = existingEntity.UpdateTime(operation)
			if err != nil {
				return nil, err
			}
		}

		deletedEntity, err = existingEntity.Delete(data)
		if err != nil {
			return nil, err
		}

		if deletedEntity == nil {
			return nil, NewDomainError("error deleting entity.", entityType, entityID, nil)
		}

	}
	return deletedEntity, nil
}

func (s *DomainService) ValidateUnique(ctx context.Context, entity *ContentEntity) error {
	entityFactory := GetEntityFactory(ctx)
	if entity.Schema == nil {
		return nil
	}
	for name, p := range entity.Schema.Properties {
		uniquebytes, _ := json.Marshal(p.Value.Extensions["x-unique"])
		if len(uniquebytes) != 0 {
			unique := false
			json.Unmarshal(uniquebytes, &unique)
			if unique {
				if val, ok := entity.ToMap()[name]; ok {
					result, err := s.Projection.GetByProperties(ctx, entityFactory, map[string]interface{}{name: val})
					if err != nil {
						return NewDomainError(err.Error(), entityFactory.Name(), entity.ID, err)
					}
					if len(result) > 1 {
						err := fmt.Errorf("entity value %s should be unique but an entity exists with this %s value", name, name)
						s.logger.Debug(err)
						return NewDomainError(err.Error(), entityFactory.Name(), entity.ID, err)
					}
					if len(result) == 1 {
						r := result[0]
						if r.ID != entity.GetID() {
							err := fmt.Errorf("entity value %s should be unique but an entity exists with this %s value", name, name)
							s.logger.Debug(err)
							return NewDomainError(err.Error(), entityFactory.Name(), entity.ID, err)
						}
					}
				}
			}
		}
	}
	return nil
}

func NewDomainService(ctx context.Context, eventRepository EventRepository, projections Projection, logger Log) *DomainService {
	return &DomainService{
		eventRepository: eventRepository,
		Projection:      projections,
		logger:          logger,
	}
}
