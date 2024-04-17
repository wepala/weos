package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"net/http"
)

type Event struct {
	gorm.Model
	Type    string            `json:"type"`
	Payload datatypes.JSONMap `json:"payload"`
	Meta    EventMeta         `json:"meta" gorm:"embedded"`
	Version int               `json:"version"`
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

type EventOptions struct {
	ResourceRepository *ResourceRepository
	DefaultProjection  Projection
	Projections        map[string]Projection
	HttpClient         *http.Client
	GORMDB             *gorm.DB
	Request            *http.Request
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

func (e *Event) GetSequenceNo() int64 {
	//TODO implement me
	panic("implement me")
}

func (e *Event) GetID() string {
	//TODO implement me
	panic("implement me")
}

func (e *Event) FromBytes(schema *openapi3.T, data []byte) (Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (e *Event) IsValid() bool {
	//TODO implement me
	panic("implement me")
}

func (e *Event) GetErrors() []error {
	//TODO implement me
	panic("implement me")
}
