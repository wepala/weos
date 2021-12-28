package model

import (
	"github.com/getkin/kin-openapi/openapi3"
	"golang.org/x/net/context"
)

type ContentAggregateInterface interface {
	IsValid() bool
	FromSchema(ctx context.Context, schema openapi3.Schema)
}
type ContentAggregateRoot struct {
	AggregateRoot
	Schema openapi3.Schema
}

func (w *ContentAggregateRoot) IsValid(entityType) bool {

	return false
}

func (w *ContentAggregateRoot) FromSchema(ctx context.Context, schema *openapi3.Schema) (ContentAggregateInterface, error) {
	//dynamicstruct.NewStruct().AddField().AddField().Build().New()

	return nil, nil
}
