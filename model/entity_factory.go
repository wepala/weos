package model

import (
	"github.com/getkin/kin-openapi/openapi3"
	ds "github.com/ompluscator/dynamic-struct"
	weosContext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
	"strings"
)

type EntityFactory interface {
	FromSchemaAndBuilder(string, *openapi3.Schema, ds.Builder) EntityFactory
	NewEntity(ctx context.Context) (*ContentEntity, error)
	//CreateEntityWithValues add an entity for the first type to the system with the following values
	CreateEntityWithValues(ctx context.Context, payload []byte) (*ContentEntity, error)
	DynamicStruct(ctx context.Context) ds.DynamicStruct
	Name() string
	TableName() string
	Schema() *openapi3.Schema
	Builder(ctx context.Context) ds.Builder
}

type DefaultEntityFactory struct {
	name    string
	schema  *openapi3.Schema
	builder ds.Builder
}

//Deprecated: 06/04/2022 the builder should not be needed
//FromSchemaAndBuilder create entity factory using a schema and dynamic struct builder
func (d *DefaultEntityFactory) FromSchemaAndBuilder(s string, o *openapi3.Schema, builder ds.Builder) EntityFactory {
	d.schema = o
	d.builder = builder
	d.name = s
	return d
}

func (d *DefaultEntityFactory) FromSchema(s string, o *openapi3.Schema) EntityFactory {
	d.schema = o
	d.name = s
	return d
}

func (d *DefaultEntityFactory) NewEntity(ctxt context.Context) (*ContentEntity, error) {
	if d.builder != nil {
		return new(ContentEntity).FromSchemaAndBuilder(ctxt, d.schema, d.builder)
	}
	return new(ContentEntity).FromSchema(ctxt, d.schema)
}

func (d *DefaultEntityFactory) CreateEntityWithValues(ctxt context.Context, payload []byte) (*ContentEntity, error) {
	var entity *ContentEntity
	var err error
	if d.builder != nil {
		entity, err = new(ContentEntity).FromSchemaAndBuilder(ctxt, d.schema, d.builder)
	} else {
		entity, err = new(ContentEntity).FromSchema(ctxt, d.schema)
	}

	if err != nil {
		return nil, err
	}
	if id, ok := ctxt.Value(weosContext.WEOS_ID).(string); ok {
		entity.ID = id
	}
	return entity.Init(ctxt, payload)
}

func (d *DefaultEntityFactory) Name() string {
	return d.name
}

func (d *DefaultEntityFactory) TableName() string {
	return strings.Title(d.Name())
}

func (d *DefaultEntityFactory) Schema() *openapi3.Schema {
	return d.schema
}

func (d *DefaultEntityFactory) DynamicStruct(ctx context.Context) ds.DynamicStruct {
	return d.builder.Build()
}

func (d *DefaultEntityFactory) Builder(ctx context.Context) ds.Builder {
	return d.builder
}

//GetEntityFactory get entity factory from context
func GetEntityFactory(ctx context.Context) EntityFactory {
	if value, ok := ctx.Value(weosContext.ENTITY_FACTORY).(EntityFactory); ok {
		return value
	}
	return nil
}
