package model

import (
	"encoding/json"
	"github.com/segmentio/ksuid"
	"time"
)

const CREATE_EVENT = "create"

type Event struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	Meta    EventMeta       `json:"meta"`
	Version int             `json:"version"`
	errors  []error
}

//NewBasicEvent Create a basic event
//Deprecated: 08/12/2021 This factory doesn't take into account the Aggregate root which was just introduced. Use NewEntityEvent instead
func NewBasicEvent(eventType string, entityID string, entityType string, payload interface{}) (*Event, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, NewDomainError("Unable to marshal event payload", eventType, entityID, err)
	}
	return &Event{
		ID:      ksuid.New().String(),
		Type:    eventType,
		Payload: payloadBytes,
		Version: 1,
		Meta: EventMeta{
			EntityID:   entityID,
			EntityType: entityType,
			Created:    time.Now().Format(time.RFC3339Nano),
		},
	}, nil
}

//NewAggregateEvent generates an event on a root aggregate.
//Deprecated: 08/12/2021 This factory doesn't take into account the Aggregate root which was just introduced. Use NewEntityEvent instead
func NewAggregateEvent(eventType string, entity Entity, payload interface{}) *Event {
	payloadBytes, _ := json.Marshal(payload)
	return &Event{
		ID:      ksuid.New().String(),
		Type:    eventType,
		Payload: payloadBytes,
		Version: 1,
		Meta: EventMeta{
			EntityID:   entity.GetID(),
			EntityType: GetType(entity),
			Created:    time.Now().Format(time.RFC3339Nano),
		},
	}
}

//NewEntityEvent Creates an event for an entity within a root aggregate.
//The rootID is passed in (as opposed to the root entity) to improve developer experience
func NewEntityEvent(eventType string, entity Entity, rootID string, tpayload interface{}) *Event {
	var ok bool
	var payload json.RawMessage
	if payload, ok = tpayload.([]byte); !ok {
		payload, _ = json.Marshal(tpayload)
	}

	return &Event{
		ID:      ksuid.New().String(),
		Type:    eventType,
		Payload: payload,
		Version: 1,
		Meta: EventMeta{
			EntityID:   entity.GetID(),
			EntityType: GetType(entity),
			RootID:     rootID,
			Created:    time.Now().Format(time.RFC3339Nano),
		},
	}
}

func NewWebEntityEvent(eventType string, entity Entity, tpayload interface{}) *Event {
	var ok bool
	var payload json.RawMessage
	if payload, ok = tpayload.([]byte); !ok {
		payload, _ = json.Marshal(tpayload)
	}

	return &Event{
		ID:      ksuid.New().String(),
		Type:    eventType,
		Payload: payload,
		Version: 1,
		Meta: EventMeta{
			EntityID:   entity.GetID(),
			EntityType: GetType(entity),
			Created:    time.Now().Format(time.RFC3339Nano),
		},
	}
}

var NewVersionEvent = func(eventType string, entityID string, payload interface{}, version int) (*Event, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, NewDomainError("Unable to marshal event payload", eventType, entityID, err)
	}
	return &Event{
		ID:      ksuid.New().String(),
		Type:    eventType,
		Payload: payloadBytes,
		Version: version,
		Meta: EventMeta{
			EntityID: entityID,
			Created:  time.Now().Format(time.RFC3339Nano),
		},
	}, nil
}

type EventMeta struct {
	EntityID   string `json:"entity_id"`
	EntityPath string `json:"resource_path"`
	EntityType string `json:"entity_type"`
	SequenceNo int64  `json:"sequence_no"`
	User       string `json:"user"`
	Module     string `json:"module"`
	RootID     string `json:"root_id"`
	Group      string `json:"group"`
	Created    string `json:"created"`
}

func (e *Event) IsValid() bool {
	if e.ID == "" {
		e.AddError(NewDomainError("all events must have an id", "Event", e.Meta.EntityID, nil))
		return false
	}

	if e.Meta.EntityID == "" {
		e.AddError(NewDomainError("all domain events must be associated with an entity", "Event", e.Meta.EntityID, nil))
		return false
	}

	if e.Version == 0 {
		e.AddError(NewDomainError("all domain events must have a version no.", "Event", e.Meta.EntityID, nil))
		return false
	}

	if e.Type == "" {
		e.AddError(NewDomainError("all domain events must have a type", "Event", e.Meta.EntityID, nil))
		return false
	}

	if e.Meta.EntityType == "" {
		e.AddError(NewDomainError("all domain events must have an entity type", "Event", e.Meta.EntityID, nil))
		return false
	}

	return true
}

func (e *Event) AddError(err error) {
	e.errors = append(e.errors, err)
}

func (e *Event) GetErrors() []error {
	return e.errors
}

func (e *Event) GetID() string {
	return e.ID
}
