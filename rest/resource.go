package rest

import (
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"gorm.io/datatypes"
	"time"
)

type EventSourced interface {
	NewChange(event *Event)
	GetNewChanges() []Resource
	Persist()
}

type Resource interface {
	EventSourced
	GetType() string
	GetSequenceNo() int64
	GetID() string
	FromBytes(schema *openapi3.T, data []byte) (Resource, error)
	IsValid() bool
	GetErrors() []error
}

type BasicResource struct {
	Body      datatypes.JSONMap `gorm:"default:'[]'; null'"`
	Metadata  ResourceMetadata  `gorm:"embedded"`
	newEvents []Resource        `gorm:"-"`
}

type ResourceMetadata struct {
	ID         string `gorm:"primaryKey"`
	SequenceNo int64
	Type       string
	Version    int
	UserID     string
	AccountID  string
}

// MarshalJSON customizes the JSON encoding of BasicResource
func (r *BasicResource) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.Body)
}

// UnmarshalJSON customizes the JSON decoding of BasicResource
func (r *BasicResource) UnmarshalJSON(data []byte) (err error) {
	// You might want to initialize your map if it's nil
	if r.Body == nil {
		r.Body = make(map[string]interface{})
		r.Body["@context"] = map[string]interface{}{}
	}
	//created a temporary map to hold the data because the JSONMAP creates an empty map in it's own UnmarshalJSON
	var tbody map[string]interface{}
	tbody = map[string]interface{}(r.Body)
	// Unmarshal data into the map
	err = json.Unmarshal(data, &tbody)
	if ttype, ok := tbody["@type"].(string); ok {
		r.Metadata.Type = ttype
	}

	if id, ok := tbody["@id"].(string); ok {
		r.Metadata.ID = id
	}
	r.Body = tbody
	return err
}

// FromBytes creates a new BasicResource from a schema and data
func (r *BasicResource) FromBytes(schema *openapi3.T, data []byte) (Resource, error) {
	err := r.UnmarshalJSON(data)
	//TODO use the schema to validate the data
	//TODO fill in any missing blanks
	if r.GetType() == "" {
		return nil, fmt.Errorf("missing type")
	}
	eventType := "create"
	if r.GetSequenceNo() > 0 {
		eventType = "update"
	}
	r.NewChange(NewResourceEvent(eventType, r, r.Body))
	return r, err
}

func (r *BasicResource) IsValid() bool {
	//TODO this should use the matched schema from the OpenAPI spec to validate the resource
	panic("implement me")
}

func (r *BasicResource) AddError(err error) {
	//TODO implement me
	panic("implement me")
}

func (r *BasicResource) GetErrors() []error {
	//TODO implement me
	panic("implement me")
}

func (r *BasicResource) GetID() string {
	return r.Metadata.ID
}

func (r *BasicResource) GetType() string {
	return r.Metadata.Type
}

func (r *BasicResource) GetSequenceNo() int64 {
	return r.Metadata.SequenceNo
}

// NewChange adds a new event to the list of new events
func (r *BasicResource) NewChange(event *Event) {
	r.Metadata.SequenceNo += 1
	event.Meta.SequenceNo = r.Metadata.SequenceNo
	r.newEvents = append(r.newEvents, event)
}

// GetNewChanges returns the list of new events
func (r *BasicResource) GetNewChanges() []Resource {
	return r.newEvents
}

// Persist clears the new events array
func (r *BasicResource) Persist() {
	r.newEvents = nil
}

func (r *BasicResource) GetString(propertyName string) string {
	if value, ok := r.Body[propertyName].(string); ok {
		return value
	}
	return ""
}

func (r *BasicResource) GetBool(propertyName string) bool {
	if value, ok := r.Body[propertyName].(bool); ok {
		return value
	}
	return false
}

func (r *BasicResource) GetInt(propertyName string) int {
	if value, ok := r.Body[propertyName].(int); ok {
		return value
	}
	return 0
}

func (r *BasicResource) GetFloat(propertyName string) float64 {
	if value, ok := r.Body[propertyName].(float64); ok {
		return value
	}
	return 0.0
}

func NewResourceEvent(eventType string, resource Resource, tpayload map[string]interface{}) *Event {
	var payload json.RawMessage
	payload, _ = json.Marshal(tpayload)

	return &Event{
		Type:    eventType,
		Payload: payload,
		Version: 2,
		Meta: EventMeta{
			ResourceID:   resource.GetID(),
			ResourceType: resource.GetType(),
			Created:      time.Now().Format(time.RFC3339Nano),
		},
	}
}
