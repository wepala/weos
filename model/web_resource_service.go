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
	serverUrl  string
	repository Projection
}

//CreateSchema add schema to web resources
func (s *WebResourceService) CreateSchema(ctxt context.Context, path string, schema *openapi3.Schema) (WebResource, error) {
	var schemaWebResource WebResource
	var ok bool
	//check if there is already a schema web resource
	schemaEntity, err := s.repository.GetByID(ctxt, s.serverUrl+SCHEMA_SEGMENT+path)
	if err != nil {
		return nil, err
	}

	//if there is already an schema then return an domain error
	if schemaEntity != nil {
		return nil, NewDomainError(fmt.Sprintf("a schema for the path '%s' already exists", path), "WebResource", "", nil)
	}

	//convert schema web resource to open api schema
	schema := &openapi3.Schema{}
	if schemaWebResource, ok = schemaEntity.(WebResource); ok {
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

	schema = SchemaFromPayload(payload)
	//TODO dispatch command to create web resource for schema
	//schemaWebResource = make(WebResource)
	//schemaJSON, err := json.Marshal(schema)
	//if err != nil {
	//	return entity,err
	//}

	return nil, nil
}

func (s *WebResourceService) Create(ctxt context.Context, path string, payloadType string, payload []byte, schema *openapi3.Schema) (WebResource, error) {
	entity := make(WebResource)

	//if no schema is passed AND this path is itself is not for creating a segment
	if schema == nil && !strings.Contains(path, SCHEMA_SEGMENT) {

	}

	//update schema

	entity = entity.FromSchema(ctxt, schema).(WebResource)

	switch payloadType {
	default:
		entity = entity.FromJSON(ctxt, path, payload, schema).(WebResource)
	}
	return entity, nil
}

func (s *WebResourceService) GetByURL() {

}

func NewWebResourceService(serverUrl string, repository Projection) *WebResourceService {
	return &WebResourceService{
		serverUrl:  strings.TrimRight(serverUrl, "/"),
		repository: repository,
	}
}
