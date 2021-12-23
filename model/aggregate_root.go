package model

import (
	"encoding/json"
)

type AggregateInterface interface {
	NewChange(event *Event)
	GetNewChanges() []Entity
	Persist()
}

//AggregateRoot Is a base struct for WeOS applications to use. This is event sourcing ready by default
type AggregateRoot struct {
	BasicEntity
	SequenceNo int64
	newEvents  []Entity
	User       User
}

func (w *AggregateRoot) GetUser() User {
	return w.User
}

func (w *AggregateRoot) SetUser(user User) {
	w.User = user
}

func (w *AggregateRoot) NewChange(event *Event) {
	w.SequenceNo += 1
	event.Meta.SequenceNo = w.SequenceNo
	w.newEvents = append(w.newEvents, event)
}

func (w *AggregateRoot) GetNewChanges() []Entity {
	return w.newEvents
}

//Persist clears the new events array
func (w *AggregateRoot) Persist() {
	w.newEvents = nil
}

var DefaultReducer = func(initialState Entity, event *Event, next Reducer) Entity {
	//convert event to json string
	eventString, err := json.Marshal(event.Payload)
	if err != nil {
		initialState.AddError(NewDomainError("error marshalling event", "", initialState.GetID(), err))
	} else {
		err := json.Unmarshal(eventString, &initialState)
		if err != nil {
			initialState.AddError(NewDomainError("error unmarshalling event into entity", "", initialState.GetID(), err))
		}
	}
	//if it's an aggregate root then let's set the user and account based on the event meta details
	if aggregateRoot, ok := initialState.(WeOSEntity); ok {
		aggregateRoot.SetUser(User{
			BasicEntity{
				ID: event.Meta.User,
			},
		})
	}

	return initialState
}

var NewAggregateFromEvents = func(initialState Entity, events []*Event) Entity {
	for _, event := range events {
		initialState = DefaultReducer(initialState, event, nil)
	}

	return initialState
}
