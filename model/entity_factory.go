package model

import (
	"github.com/getkin/kin-openapi/openapi3"
	ds "github.com/ompluscator/dynamic-struct"
	weosContext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
)

type EntityFactory interface {
	FromSchemaAndBuilder(string, *openapi3.Schema, ds.Builder) EntityFactory
	NewEntity(ctx context.Context) (*ContentEntity, error)
	Name() string
}

type DefaultEntityFactory struct {
	name    string
	schema  *openapi3.Schema
	builder ds.Builder
}

func (d *DefaultEntityFactory) FromSchemaAndBuilder(s string, o *openapi3.Schema, builder ds.Builder) EntityFactory {
	d.schema = o
	d.builder = builder
	d.name = s
	return d
}

func (d *DefaultEntityFactory) NewEntity(ctxt context.Context) (*ContentEntity, error) {
	return new(ContentEntity).FromSchemaAndBuilder(ctxt, d.schema, d.builder)
}

func (d *DefaultEntityFactory) Name() string {
	return d.name
}

//GetEntityFactory get entity factory from context
func GetEntityFactory(ctx context.Context) EntityFactory {
	if value, ok := ctx.Value(weosContext.ENTITY_FACTORY).(EntityFactory); ok {
		return value
	}
	return nil
}
