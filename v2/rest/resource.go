package rest

import (
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/segmentio/ksuid"
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
	GetSequenceNo() int
	GetID() string
}

type BasicResource struct {
	body      map[string]interface{}
	Metadata  ResourceMetadata
	newEvents []Resource
}

type ResourceMetadata struct {
	ID         string
	SequenceNo int
	Type       string
	Version    int64
	UserID     string
	AccountID  string
}

// MarshalJSON customizes the JSON encoding of BasicResource
func (r *BasicResource) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.body)
}

// UnmarshalJSON customizes the JSON decoding of BasicResource
func (r *BasicResource) UnmarshalJSON(data []byte) error {
	// You might want to initialize your map if it's nil
	if r.body == nil {
		r.body = make(map[string]interface{})
		r.body["@context"] = map[string]interface{}{}
		r.body["@type"] = ""
	}
	// Unmarshal data into the map
	return json.Unmarshal(data, &r.body)
}

// FromSchema creates a new BasicResource from a schema and data
func (r *BasicResource) FromSchema(schema *openapi3.T, data []byte) (*BasicResource, error) {
	err := r.UnmarshalJSON(data)
	//TODO use the schema to validate the data
	//TODO fill in any missing blanks
	if r.GetType() == "" {
		return nil, fmt.Errorf("missing type")
	}
	r.NewChange(NewResourceEvent("create", r, r.body))
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
	if id, ok := r.body["@id"].(string); ok {
		return id
	}
	return ""
}

func (r *BasicResource) GetType() string {
	if ttype, ok := r.body["@type"].(string); ok {
		return ttype
	}
	return ""
}

func (r *BasicResource) GetSequenceNo() int {
	return r.Metadata.SequenceNo
}

// NewChange adds a new event to the list of new events
func (r *BasicResource) NewChange(event *Event) {
	r.Metadata.SequenceNo += 1
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

func NewResourceEvent(eventType string, resource Resource, tpayload map[string]interface{}) *Event {
	var payload json.RawMessage
	payload, _ = json.Marshal(tpayload)

	return &Event{
		ID:      ksuid.New().String(),
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
