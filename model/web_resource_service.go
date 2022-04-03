package model

import (
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"golang.org/x/net/context"
	"strings"
)

const SCHEMA_SEGMENT = "/_segment"

//WebResourceService use to manage web resources
type WebResourceService struct {
	serverUrl         string
	repository        Projection
	CommandDispatcher CommandDispatcher
}

func (s *WebResourceService) Create(ctxt context.Context, path string, payloadType string, payload []byte, schema *openapi3.Schema) (WebEntity, error) {
	entity := make(WebEntity)
	var schemaWebResource WebEntity
	var ok bool

	//if no schema is passed AND this path is itself is not for creating a segment
	if schema == nil && !strings.Contains(path, SCHEMA_SEGMENT) {
		//check if there is already a schema web resource
		schemaEntity, err := s.repository.GetByID(ctxt, s.serverUrl+SCHEMA_SEGMENT+path)
		if err != nil {
			return entity, err
		}

		//convert schema web resource to open api schema
		schema = &openapi3.Schema{}
		if schemaWebResource, ok = schemaEntity.(WebEntity); ok {
			schemaBytes, err := json.Marshal(schemaWebResource)
			if err != nil {
				return nil, fmt.Errorf("error setting up schema '%s'", err)
			}
			err = json.Unmarshal(schemaBytes, schema)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling schema '%s'", err)
			}
		}
		//if there isn't a web resource for the schema then let's create the schema and save it as a web resource
		if schema == nil {
			schema = SchemaFromPayload(payload)
			//TODO dispatch command to create web resource for schema
			//schemaWebResource = make(WebEntity)
			//schemaJSON, err := json.Marshal(schema)
			//if err != nil {
			//	return entity,err
			//}

		}
	}

	//update schema

	entity = entity.FromSchema(ctxt, schema).(WebEntity)

	switch payloadType {
	default:
		entity = entity.FromJSON(ctxt, path, payload, schema).(WebEntity)
	}
	return entity, nil
}

func (s *WebResourceService) GetByURL() {

}

func NewWebResourceService(serverUrl string, repository Projection, dispatcher CommandDispatcher) *WebResourceService {
	return &WebResourceService{
		serverUrl:         strings.TrimRight(serverUrl, "/"),
		repository:        repository,
		CommandDispatcher: dispatcher,
	}
}
