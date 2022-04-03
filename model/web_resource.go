package model

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"golang.org/x/net/context"
)

type WebEntity map[string]interface{}

func (e WebEntity) IsValid() bool {
	//TODO implement me
	panic("implement me")
}

func (e WebEntity) AddError(err error) {
	//TODO implement me
	panic("implement me")
}

func (e WebEntity) GetErrors() []error {
	//TODO implement me
	panic("implement me")
}

func (e WebEntity) GetID() string {
	return e.ID()
}

//ID returns the id of the resource e.g. wern:blog:27HulTkipdvprPRVgIgca07UTGr
func (e WebEntity) ID() string {
	//TODO implement me
	panic("implement me")
}

func (e WebEntity) Path() string {
	if path, ok := e["_path"]; ok {
		if value, ok := path.(string); ok {
			return value
		}
	}
	return ""
}

func (e WebEntity) URI() string {
	if url, ok := e["@id"]; ok {
		if value, ok := url.(string); ok {
			return value
		}
	}
	return ""
}

func (e WebEntity) URN() string {
	if url, ok := e["weos_id"]; ok {
		if value, ok := url.(string); ok {
			return value
		}
	}
	return ""
}

func (e WebEntity) Type() string {
	if url, ok := e["@type"]; ok {
		if value, ok := url.(string); ok {
			return value
		}
	}
	return ""
}

func (e WebEntity) Types() []string {
	if url, ok := e["@type"]; ok {
		if value, ok := url.([]string); ok {
			return value
		}
	}
	return []string{}
}

func (e WebEntity) Schema() *openapi3.Schema {
	if url, ok := e["@schema"]; ok {
		if value, ok := url.(*openapi3.Schema); ok {
			return value
		}
	}
	//TODO use type(s) to create a schema
	return nil
}

func (e WebEntity) FromSchema(ctx context.Context, ref *openapi3.Schema) Entity {
	return e
}

func (e WebEntity) FromJSON(ctx context.Context, path string, payload []byte, ref *openapi3.Schema) Entity {
	return e
}

func (e WebEntity) FromJSONLD(ctx context.Context, ref *openapi3.Schema) Entity {
	return e
}

func (e WebEntity) Create(payload json.RawMessage) {
	//TODO if there is no @id create one using id
	//TODO if id is empty generate one using the schema rules

}
