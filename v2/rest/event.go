package rest

import (
	"encoding/json"
)

type Event struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	Meta    EventMeta       `json:"meta"`
	Version int             `json:"version"`
	errors  []error
}

type EventMeta struct {
	ResourceID    string `json:"resourceId"`
	ResourceType  string `json:"resourceType"`
	SequenceNo    int64  `json:"sequenceNo"`
	User          string `json:"user"`
	ApplicationID string `json:"applicationId"`
	RootID        string `json:"rootId"`
	AccountID     string `json:"accountId"`
	Created       string `json:"created"`
}

func (e *Event) NewChange(event *Event) {
	//TODO implement me
	panic("implement me")
}

func (e *Event) GetNewChanges() []Resource {
	//TODO implement me
	panic("implement me")
}

func (e *Event) Persist() {
	//TODO implement me
	panic("implement me")
}

func (e *Event) GetType() string {
	//TODO implement me
	panic("implement me")
}

func (e *Event) GetSequenceNo() int {
	//TODO implement me
	panic("implement me")
}

func (e *Event) GetID() string {
	//TODO implement me
	panic("implement me")
}
