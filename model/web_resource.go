package model

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"golang.org/x/net/context"
)

type WebResource map[string]interface{}

func (e WebResource) IsValid() bool {
	//TODO implement me
	panic("implement me")
}

func (e WebResource) AddError(err error) {
	//TODO implement me
	panic("implement me")
}

func (e WebResource) GetErrors() []error {
	//TODO implement me
	panic("implement me")
}

func (e WebResource) GetID() string {
	return e.ID()
}

//ID returns the id of the resource e.g. wern:blog:27HulTkipdvprPRVgIgca07UTGr
func (e WebResource) ID() string {
	//TODO implement me
	panic("implement me")
}

func (e WebResource) Path() string {
	if path, ok := e["_path"]; ok {
		if value, ok := path.(string); ok {
			return value
		}
	}
	return ""
}

func (e WebResource) URI() string {
	if url, ok := e["@id"]; ok {
		if value, ok := url.(string); ok {
			return value
		}
	}
	return ""
}

func (e WebResource) URN() string {
	if url, ok := e["weos_id"]; ok {
		if value, ok := url.(string); ok {
			return value
		}
	}
	return ""
}

func (e WebResource) Type() string {
	if url, ok := e["@type"]; ok {
		if value, ok := url.(string); ok {
			return value
		}
	}
	return ""
}

func (e WebResource) Types() []string {
	if url, ok := e["@type"]; ok {
		if value, ok := url.([]string); ok {
			return value
		}
	}
	return []string{}
}

func (e WebResource) Schema() *openapi3.Schema {
	if url, ok := e["@schema"]; ok {
		if value, ok := url.(*openapi3.Schema); ok {
			return value
		}
	}
	//TODO use type(s) to create a schema
	return nil
}

func (e WebResource) FromSchema(ctx context.Context, ref *openapi3.Schema) Entity {
	return e
}

func (e WebResource) FromJSON(ctx context.Context, path string, payload []byte, ref *openapi3.Schema) Entity {
	return e
}

func (e WebResource) FromJSONLD(ctx context.Context, ref *openapi3.Schema) Entity {
	return e
}

func (e WebResource) Create(payload json.RawMessage) {
	//TODO if there is no @id create one using id
	//TODO if id is empty generate one using the schema rules

}
