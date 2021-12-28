package model

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	ds "github.com/ompluscator/dynamic-struct"
	"golang.org/x/net/context"
)

type ContentEntity struct {
	AggregateRoot
	Schema *openapi3.Schema
}

func (w *ContentEntity) IsValid() bool {

	return false
}

func (w *ContentEntity) FromSchema(ctx context.Context, schema *openapi3.Schema) (*ContentEntity, error) {
	instance := ds.ExtendStruct()
	return &ContentEntity{
		Schema: schema,
	}, nil

}

func (w *ContentEntity) FromSchemaWithValues(ctx context.Context, schema *openapi3.Schema, payload json.RawMessage) (*ContentEntity, error) {
	return nil, nil
}
func (w *ContentEntity) GetString(name string) string {
	//TODO use reflection to get string properties on the object
	return ""
}
