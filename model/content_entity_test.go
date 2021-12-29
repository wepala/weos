package model_test

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/wepala/weos-service/model"
	"golang.org/x/net/context"
	"testing"
)

func TestContentEntity_FromSchema(t *testing.T) {
	//load open api spec
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	contentType = "Blog"
	contentTypeSchema = swagger.Components.Schemas[contentType]
	ctx := context.Background()
	ctx = context.WithValue(ctx, weosContext.CONTENT_TYPE, &weosContext.ContentType{
		Name:   contentType,
		Schema: contentTypeSchema.Value,
	})
	ctx = context.WithValue(ctx, weosContext.USER_ID, "123")
	entity, err := new(model.ContentEntity).FromSchema(ctx, swagger.Components.Schemas["Blog"].Value)
	if err != nil {
		t.Fatalf("unexpected error instantiating content entity '%s'", err)
	}

	if entity.Property == nil {
		t.Fatal("expected item to be returned")
	}

	if entity.GetString("Title") != "" {
		t.Errorf("expected there to be a field '%s' with value '%s' got '%s'", "Title", " ", entity.GetString("Title"))
	}
}

func TestContentEntity_IsValid(t *testing.T) {
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	contentType = "Blog"
	contentTypeSchema = swagger.Components.Schemas[contentType]
	ctx := context.Background()
	ctx = context.WithValue(ctx, weosContext.CONTENT_TYPE, &weosContext.ContentType{
		Name:   contentType,
		Schema: contentTypeSchema.Value,
	})
	ctx = context.WithValue(ctx, weosContext.USER_ID, "123")
	mockBlog := &Blog{
		Title:       "test 1",
		Description: "lorem ipsum",
	}
	payload, err := json.Marshal(mockBlog)
	if err != nil {
		t.Fatalf("unexpected error marshalling payload '%s'", err)
	}

	entity, err := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Blog"].Value, payload)
	if err != nil {
		t.Fatalf("unexpected error instantiating content entity '%s'", err)
	}
	if entity.Property == nil {
		t.Fatal("expected item to be returned")
	}

	if entity.GetString("Title") != "test 1" {
		t.Errorf("expected the title to be '%s', got '%s'", "test 1", entity.GetString("Title"))
	}
}
