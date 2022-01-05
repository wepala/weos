package model_test

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	weosContext "github.com/wepala/weos-service/context"
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

type TestBlog struct {
	ID          string  `json:"id"`
	Title       *string `json:"title"`
	Description string  `json:"description"`
	Url         string  `json:"url"`
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
	t.Run("Testing with all the required fields", func(t *testing.T) {
		mockBlog := &Blog{
			Title:       "test 1",
			Description: "lorem ipsum",
			Url:         "www.ShaniahsBlog.com",
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
		isValid := entity.IsValid()
		if !isValid {
			t.Fatalf("unexpected error expected entity to be valid got invalid")
		}
	})
	t.Run("Testing with a missing required field that is nullable: title", func(t *testing.T) {
		mockBlog := &TestBlog{
			Description: "lorem ipsum",
			Url:         "www.ShaniahsBlog.com",
		}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("unexpected error marshalling payload '%s'", err)
		}

		entity, err := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Blog"].Value, payload)
		if err != nil {
			t.Fatalf("unexpected error while instantiating content entity got '%s'", err)
		}
		isValid := entity.IsValid()
		if isValid {
			t.Fatalf("expected entity to be invalid got valid")
		}
	})
}
