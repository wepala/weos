package model_test

import (
	"crypto/sha256"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/segmentio/ksuid"
	weosContext "github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/sha3"
	"golang.org/x/net/context"
	"testing"
	"time"
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

func TestContentEntity_Init(t *testing.T) {
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
	builder := rest.CreateSchema(ctx, echo.New(), swagger)

	blog := make(map[string]interface{})
	blog["title"] = "Test"
	payload, err := json.Marshal(blog)
	if err != nil {
		t.Fatalf("unexpected error marshalling payload '%s'", err)
	}

	entity, err := new(model.ContentEntity).FromSchemaAndBuilder(ctx, swagger.Components.Schemas["Blog"].Value, builder[contentType])
	if err != nil {
		t.Fatalf("unexpected error instantiating content entity '%s'", err)
	}

	entity.Init(ctx, payload)

	if entity.Property == nil {
		t.Fatal("expected item to be returned")
	}

	if entity.GetString("Title") == "" {
		t.Errorf("expected there to be a field '%s' with value '%s' got '%s'", blog["title"], " ", entity.GetString("Title"))
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
	t.Run("Testing with all the required fields", func(t *testing.T) {
		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com"}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
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
		mockBlog := map[string]interface{}{"description": "New Description", "url": "www.NewBlog.com"}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		entity, err := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Blog"].Value, payload)
		if err != nil {
			t.Fatalf("unexpected error while instantiating content entity got '%s'", err)
		}
		isValid := entity.IsValid()
		if isValid {
			t.Fatalf("expected entity to be invalid got valid")
		}
		if len(entity.GetErrors()) == 0 {
			t.Fatalf("expected entity have errors got none")
		}
		for _, err := range entity.GetErrors() {
			if errr, ok := err.(*model.DomainError); !ok {
				t.Fatalf("expected domain error got %s", errr)
			}
		}
	})
}

func TestContentEntity_Update(t *testing.T) {
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	ctx := context.Background()
	contentType := "Blog"
	schema := swagger.Components.Schemas[contentType].Value
	builder := rest.CreateSchema(ctx, echo.New(), swagger)

	ctx = context.WithValue(ctx, weosContext.USER_ID, "123")

	mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com"}
	payload, err := json.Marshal(mockBlog)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}
	existingEntity := &model.ContentEntity{}
	existingEntity, err = existingEntity.FromSchemaAndBuilder(ctx, schema, builder[contentType])
	err = existingEntity.SetValueFromPayload(ctx, payload)
	if err != nil {
		t.Fatalf("unexpected error instantiating content entity '%s'", err)
	}

	if existingEntity.GetString("Title") != "test 1" {
		t.Errorf("expected the title to be '%s', got '%s'", "test 1", existingEntity.GetString("Title"))
	}

	updatedBlog := map[string]interface{}{"title": "Updated title", "description": "Updated Description", "url": "www.UpdatedBlog.com"}
	updatedPayload, err := json.Marshal(updatedBlog)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}

	updatedEntity, err := existingEntity.Update(ctx, updatedPayload)
	if err != nil {
		t.Fatalf("unexpected error updating existing entity '%s'", err)
	}

	if updatedEntity.GetString("Title") != "Updated title" {
		t.Errorf("expected the updated title to be '%s', got '%s'", "Updated title", existingEntity.GetString("Title"))
	}

	if updatedEntity.GetString("Description") != "Updated Description" {
		t.Errorf("expected the updated description to be '%s', got '%s'", "Updated Description", existingEntity.GetString("Description"))
	}
}

func TestContentEntity_FromSchemaWithEvents(t *testing.T) {
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

	mockEvent1 := &model.Event{
		ID:      "1234sd",
		Type:    "create",
		Payload: nil,
		Meta: model.EventMeta{
			EntityID:   "1234sd",
			EntityType: "Blog",
			SequenceNo: 1,
			User:       "",
			Module:     "",
			RootID:     "",
			Group:      "",
			Created:    "",
		},
		Version: 0,
	}
	mockEvent2 := &model.Event{
		ID:      "1234sd",
		Type:    "update",
		Payload: nil,
		Meta: model.EventMeta{
			EntityID:   "1234sd",
			EntityType: "Blog",
			SequenceNo: 2,
			User:       "",
			Module:     "",
			RootID:     "",
			Group:      "",
			Created:    "",
		},
		Version: 0,
	}
	events := []*model.Event{mockEvent1, mockEvent2}

	entity, err := new(model.ContentEntity).FromSchemaWithEvents(ctx, swagger.Components.Schemas["Blog"].Value, events)
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

func TestContentEntity_ToMap(t *testing.T) {
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

	result := entity.ToMap()
	if err != nil {
		t.Fatalf("unexpected error getting map '%s'", err)
	}

	if _, ok := result["title"]; !ok {
		t.Errorf("expected '%s' to be in map", "title")
	}
}

func TestContentEntity_GetOriginalFieldName(t *testing.T) {
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
	originalName, _ := entity.GetOriginalFieldName("Title")
	if originalName != "title" {
		t.Errorf("expected the original field name for '%s' to be '%s', got '%s'", "Title", "title", originalName)
	}
}

func TestContentEntity_SetValueFromPayload(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog.yaml")
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	contentType := "Blog"
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, api.Swagger.Components.Schemas[contentType].Value, schemas[contentType])
	if err != nil {
		t.Fatalf("error setting up entity factory")
	}
	entity, err := entityFactory.NewEntity(context.TODO())
	if err != nil {
		t.Fatalf("error generating entity '%s'", err)
	}

	payloadData := &struct {
		Title string  `json:"title"`
		Cost  float64 `json:"cost"`
	}{
		Title: "Test Blog",
		Cost:  45.00,
	}
	payload, err := json.Marshal(payloadData)
	if err != nil {
		t.Fatalf("error marshalling Payload '%s'", err)
	}
	err = entity.SetValueFromPayload(context.TODO(), payload)
	if err != nil {
		t.Fatalf("error setting Payload '%s'", err)
	}

	if entity.GetString("title") != payloadData.Title {
		t.Errorf("expected the title on the entity to be '%s', got '%s'", payloadData.Title, entity.GetString("title"))
	}
	//NOTE because of marshalling and unmarshalling using a float does not yield the exact number between the two.
	if entity.GetNumber("cost") != payloadData.Cost {
		t.Errorf("expected the cost to be %f, got %f", payloadData.Cost, entity.GetNumber("cost"))
	}
}

func TestContentEntity_Delete(t *testing.T) {
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

	mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com"}
	payload, err := json.Marshal(mockBlog)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}

	existingEntity, err := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Blog"].Value, payload)
	if err != nil {
		t.Fatalf("unexpected error instantiating content entity '%s'", err)
	}

	if existingEntity.GetString("Title") != "test 1" {
		t.Errorf("expected the title to be '%s', got '%s'", "test 1", existingEntity.GetString("Title"))
	}

	deletedEntity, err := existingEntity.Delete(payload)
	if err != nil {
		t.Fatalf("unexpected error updating existing entity '%s'", err)
	}

	if deletedEntity.GetString("Title") != "test 1" {
		t.Errorf("expected the updated title to be '%s', got '%s'", "test 1", deletedEntity.GetString("Title"))
	}

	if deletedEntity.GetString("Description") != "New Description" {
		t.Errorf("expected the updated description to be '%s', got '%s'", "New Description", deletedEntity.GetString("Description"))
	}

	delEvents := deletedEntity.AggregateRoot.GetNewChanges()
	lastEvent := delEvents[len(delEvents)-1].(*model.Event)

	if lastEvent == nil {
		t.Errorf("expected there to be events on the entity")
	}

	if lastEvent.Type != "delete" {
		t.Errorf("expected the last event to be '%s', got '%s'", "delete", lastEvent.Type)
	}

}

func TestContentEntity_EnumerationString(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog.yaml")
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	contentType := "Blog"
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, api.Swagger.Components.Schemas[contentType].Value, schemas[contentType])
	if err != nil {
		t.Fatalf("error setting up entity factory")
	}

	t.Run("Testing enum with all the required fields", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": ""}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.GetString("title") != "test 1" {
			t.Errorf("expected the title on the entity to be '%s', got '%s'", "test 1", entity.GetString("title"))
		}

		if entity.GetString("status") != "" {
			t.Errorf("expected the title on the entity to be '%s', got '%s'", "", entity.GetString("status"))
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if !isValid {
			t.Fatalf("unexpected error expected entity to be valid got invalid")
		}
	})
	t.Run("Testing enum with wrong option", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": "selected"}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if isValid {
			t.Fatalf("expected entity to be invalid")
		}
	})
	t.Run("Testing enum with null enum", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": "null"}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if isValid {
			t.Fatalf("expected entity to be invalid")
		}
	})
	t.Run("Testing enum with null enum", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": 0}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err == nil {
			t.Fatalf("Expected there to be an unmarshall error")
		}
	})
}

func TestContentEntity_EnumerationString2(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog-enum.yaml")
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	contentType := "Blog"
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, api.Swagger.Components.Schemas[contentType].Value, schemas[contentType])
	if err != nil {
		t.Fatalf("error setting up entity factory")
	}

	t.Run("Testing enum with nullable set to false", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": "null"}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if isValid {
			t.Fatalf("expected entity to be invalid")
		}
	})
	t.Run("Testing enum with blank enum", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com"}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if isValid {
			t.Fatalf("expected entity to be invalid")
		}
	})
}

func TestContentEntity_EnumerationInteger(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog-enum-integer.yaml")
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	contentType := "Blog"
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, api.Swagger.Components.Schemas[contentType].Value, schemas[contentType])
	if err != nil {
		t.Fatalf("error setting up entity factory")
	}

	t.Run("Testing enum with all the required fields -status0", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": nil}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.GetString("title") != "test 1" {
			t.Errorf("expected the title on the entity to be '%s', got '%s'", "test 1", entity.GetString("title"))
		}

		if entity.GetInteger("status") != 0 {
			t.Errorf("expected the status on the entity to be '%d', got '%v'", 0, entity.GetInteger("status"))
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if !isValid {
			t.Fatalf("unexpected error expected entity to be valid got invalid")
		}
	})
	t.Run("Testing enum with all the required fields -status1", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": 1}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.GetString("title") != "test 1" {
			t.Errorf("expected the title on the entity to be '%s', got '%s'", "test 1", entity.GetString("title"))
		}

		if entity.GetInteger("Status") != 1 {
			t.Errorf("expected the status on the entity to be '%d', got '%v'", 1, entity.GetInteger("Status"))
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if !isValid {
			t.Fatalf("unexpected error expected entity to be valid got invalid")
		}
	})
	t.Run("Testing enum with wrong option -status3", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": 3}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if isValid {
			t.Fatalf("expected entity to be invalid")
		}
	})
}

func TestContentEntity_EnumerationDateTime(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog-x-schema.yaml")
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	contentType := "Blog"
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, api.Swagger.Components.Schemas[contentType].Value, schemas[contentType])
	if err != nil {
		t.Fatalf("error setting up entity factory")
	}

	t.Run("Testing enum with all the required fields", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": "0001-02-01T00:00:00Z", "guid": "123dsada"}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		tt, err := time.Parse("2006-01-02T15:04:00Z", "0001-02-01T00:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		if entity.GetTime("Status") != tt {
			t.Fatalf("expected status time to be %s got %s", tt, entity.GetTime("Status"))
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if !isValid {
			t.Fatalf("unexpected error expected entity to be valid got invalid")
		}
	})
	t.Run("Testing enum with wrong option", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": "0001-04-01T00:00:00Z", "guid": "123dsada"}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if isValid {
			t.Fatalf("expected entity to be invalid")
		}
	})
}

func TestContentEntity_EnumerationFloat(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog-pk-id.yaml")
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	contentType := "Blog"
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, api.Swagger.Components.Schemas[contentType].Value, schemas[contentType])
	if err != nil {
		t.Fatalf("error setting up entity factory")
	}

	t.Run("Testing enum with all the required fields -status0", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": nil}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.GetString("title") != "test 1" {
			t.Errorf("expected the title on the entity to be '%s', got '%s'", "test 1", entity.GetString("title"))
		}

		if entity.GetNumber("status") != 0.0 {
			t.Errorf("expected the status on the entity to be '%f', got '%v'", 0.0, entity.GetNumber("status"))
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if !isValid {
			t.Fatalf("unexpected error expected entity to be valid got invalid")
		}
	})
	t.Run("Testing enum with all the required fields -status1", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": 1.5}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.GetString("title") != "test 1" {
			t.Errorf("expected the title on the entity to be '%s', got '%s'", "test 1", entity.GetString("title"))
		}

		if entity.GetNumber("Status") != 1.5 {
			t.Errorf("expected the status on the entity to be '%f', got '%v'", 1.5, entity.GetNumber("Status"))
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if !isValid {
			t.Fatalf("unexpected error expected entity to be valid got invalid")
		}
	})
	t.Run("Testing enum with wrong option -status3", func(t *testing.T) {
		//Pass in values to the content entity
		entity, err := entityFactory.NewEntity(context.TODO())
		if err != nil {
			t.Fatalf("error generating entity '%s'", err)
		}

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": 3}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err = entity.SetValueFromPayload(context.TODO(), payload)
		if err != nil {
			t.Fatalf("error setting Payload '%s'", err)
		}

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		isValid := entity.IsValid()
		if isValid {
			t.Fatalf("expected entity to be invalid")
		}
	})
}

func TestContentEntity_AutoGeneratedID(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog-x-schema.yaml")
	if err != nil {
		t.Fatalf("unexpected error setting up api: %s", err)
	}
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	t.Run("Generating id where the is format specified is ksuid", func(t *testing.T) {
		contentType1 := "Author"
		p1 := map[string]interface{}{"firstName": "my oh my", "lastName": "name"}
		payload1, err := json.Marshal(p1)
		if err != nil {
			t.Errorf("unexpected error marshalling entity; %s", err)
		}
		entityFactory1 := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType1, api.Swagger.Components.Schemas[contentType1].Value, schemas[contentType1])
		author, err := entityFactory1.CreateEntityWithValues(context.TODO(), payload1)
		if err != nil {
			t.Errorf("unexpected error generating id; %s", err)
		}
		if !author.IsValid() {
			t.Error("expected ksuid to be generated")
			for _, errString := range author.GetErrors() {
				t.Errorf("domain error '%s'", errString)
			}
		}
		_, err = ksuid.Parse(author.GetString("id"))
		if err != nil {
			fmt.Errorf("unexpected error parsing id as ksuid: %s", err)
		}
	})
	t.Run("Generating id where the is format specified is uuid", func(t *testing.T) {
		contentType2 := "Category"
		entityFactory2 := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType2, api.Swagger.Components.Schemas[contentType2].Value, schemas[contentType2])
		p2 := map[string]interface{}{"description": "my favorite"}
		payload2, err := json.Marshal(p2)
		if err != nil {
			t.Errorf("unexpected error marshalling entity; %s", err)
		}

		category, err := entityFactory2.CreateEntityWithValues(context.TODO(), payload2)
		if !category.IsValid() {
			t.Errorf("expected uuid to be generated")
			for _, errString := range category.GetErrors() {
				t.Errorf("domain error '%s'", errString)
			}
		}
		_, err = uuid.Parse(category.GetString("id"))
		if err != nil {
			t.Errorf("unexpected error parsing id as uuid: %s", err)
		}
	})
	t.Run("Generating id type is string and the format is not specified  ", func(t *testing.T) {
		contentType3 := "Archives"
		entityFactory3 := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType3, api.Swagger.Components.Schemas[contentType3].Value, schemas[contentType3])
		p3 := map[string]interface{}{"title": "old blogs"}
		payload3, err := json.Marshal(p3)
		if err != nil {
			t.Errorf("unexpected error marshalling entity; %s", err)
		}
		_, err = entityFactory3.CreateEntityWithValues(context.TODO(), payload3)
		if err == nil {
			t.Errorf("expected error generating id")
		}
	})
}

func TestContentEntity_UpdateTime(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog.yaml")
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	contentType := "Blog"
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, api.Swagger.Components.Schemas[contentType].Value, schemas[contentType])
	if err != nil {
		t.Fatalf("error setting up entity factory")
	}
	entity, err := entityFactory.NewEntity(context.TODO())
	if err != nil {
		t.Fatalf("error generating entity '%s'", err)
	}

	mapPayload := map[string]interface{}{"title": "update time", "description": "new time", "url": "www.MyBlog.com"}
	newPayload, errr := json.Marshal(mapPayload)
	if errr != nil {
		t.Fatalf("error marshalling Payload '%s'", err)
	}

	updatedTimePayload, errrr := entity.UpdateTime("Update Blog", newPayload)
	if errrr != nil {
		t.Fatalf("error updating time payload '%s'", err)
	}

	tempPayload := map[string]interface{}{}
	json.Unmarshal(updatedTimePayload, &tempPayload)

	if tempPayload["lastUpdated"] == "" {
		t.Fatalf("expected the lastupdated field to not be blank")
	}
}

func TestContentEntity_Hash(t *testing.T) {

	t.Run("bcrypt", func(t *testing.T) {
		//load open api spec
		swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog-hash.yaml")
		if err != nil {
			t.Fatalf("unexpected error occured '%s'", err)
		}
		var contentType string
		var contentTypeSchema *openapi3.SchemaRef
		contentType = "Category"
		contentTypeSchema = swagger.Components.Schemas[contentType]
		ctx := context.Background()
		ctx = context.WithValue(ctx, weosContext.CONTENT_TYPE, &weosContext.ContentType{
			Name:   contentType,
			Schema: contentTypeSchema.Value,
		})
		ctx = context.WithValue(ctx, weosContext.USER_ID, "123")
		builder := rest.CreateSchema(ctx, echo.New(), swagger)

		category := make(map[string]interface{})
		category["title"] = "Test"
		category["description"] = "this is a bcrypt hash"
		payload, err := json.Marshal(category)
		if err != nil {
			t.Fatalf("unexpected error marshalling payload '%s'", err)
		}

		entity, err := new(model.ContentEntity).FromSchemaAndBuilder(ctx, swagger.Components.Schemas["Category"].Value, builder[contentType])
		if err != nil {
			t.Fatalf("unexpected error instantiating content entity '%s'", err)
		}

		entity.Init(ctx, payload)

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		if entity.GetString("Title") == "" {
			t.Errorf("expected there to be a field '%s' with value '%s' got '%s'", category["title"], " ", entity.GetString("Title"))
		}

		plain := category["description"].(string)

		properties := entity.ToMap()
		hashed := properties["description"].(*string)

		errr := bcrypt.CompareHashAndPassword([]byte(*hashed), []byte(plain))
		if errr != nil {
			t.Fatalf("expected the hashed password to match. Err: %s, Hashed PW:  %s, Plaintext PW: %s", errr, []byte(entity.GetString("Description")), []byte(plain))
		}
	})

	t.Run("base64, sha256, sha3-256, sha3-512", func(t *testing.T) {
		//load open api spec
		swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog-hash.yaml")
		if err != nil {
			t.Fatalf("unexpected error occured '%s'", err)
		}
		var contentType string
		var contentTypeSchema *openapi3.SchemaRef
		contentType = "Author"
		contentTypeSchema = swagger.Components.Schemas[contentType]
		ctx := context.Background()
		ctx = context.WithValue(ctx, weosContext.CONTENT_TYPE, &weosContext.ContentType{
			Name:   contentType,
			Schema: contentTypeSchema.Value,
		})
		ctx = context.WithValue(ctx, weosContext.USER_ID, "123")
		builder := rest.CreateSchema(ctx, echo.New(), swagger)

		author := make(map[string]interface{})
		author["base"] = "this is a base64 hash"
		author["firstName"] = "this is a sha256 hash"
		author["lastName"] = "this is a sha3-256 hash"
		author["email"] = "this is a sha3-512 hash"
		payload, err := json.Marshal(author)
		if err != nil {
			t.Fatalf("unexpected error marshalling payload '%s'", err)
		}

		entity, err := new(model.ContentEntity).FromSchemaAndBuilder(ctx, swagger.Components.Schemas["Author"].Value, builder[contentType])
		if err != nil {
			t.Fatalf("unexpected error instantiating content entity '%s'", err)
		}

		entity.Init(ctx, payload)

		if entity.Property == nil {
			t.Fatal("expected item to be returned")
		}

		base64_Plain := author["base"].(string)
		sha256_Plain := author["firstName"].(string)
		sha3_256_Plain := author["lastName"].(string)
		sha3_512_Plain := author["email"].(string)

		properties := entity.ToMap()
		base64_Hashed := properties["base"].(*string)
		sha256_Hashed := properties["firstName"].(*string)
		sha3_256_Hashed := properties["lastName"].(*string)
		sha3_512_Hashed := properties["email"].(*string)

		//BASE64 Check
		decb64, err := b64.URLEncoding.DecodeString(*base64_Hashed)
		if err != nil {
			t.Fatalf("unexpected error decoding hash: %s", err)
		}
		if string(decb64) != base64_Plain {
			t.Fatalf("expected the decoded password to match. Err: %s, Decoded PW:  %s, Plaintext PW: %s", err, decb64, base64_Plain)
		}

		//SHA256 Check
		hash := sha256.Sum256([]byte(sha256_Plain))
		encSHA256 := b64.StdEncoding.EncodeToString(hash[:])

		if encSHA256 != *sha256_Hashed {
			t.Fatalf("expected the encoded password to match. Err: %s, Plain To Enc PW:  %s, Encoded PW: %s", err, encSHA256, *sha256_Hashed)
		}

		//SHA3-256 Check
		hash = sha3.Sum256([]byte(sha3_256_Plain))
		encSHA3256 := b64.StdEncoding.EncodeToString(hash[:])

		if encSHA3256 != *sha3_256_Hashed {
			t.Fatalf("expected the encoded password to match. Err: %s, Plain To Enc PW:  %s, Encoded PW: %s", err, encSHA3256, *sha3_256_Hashed)
		}

		//SHA3-512 Check
		hash1 := sha3.Sum512([]byte(sha3_512_Plain))
		encSHA3512 := b64.StdEncoding.EncodeToString(hash1[:])

		if encSHA3512 != *sha3_512_Hashed {
			t.Fatalf("expected the encoded password to match. Err: %s, Plain To Enc PW:  %s, Encoded PW: %s", err, encSHA3512, *sha3_512_Hashed)
		}
	})

	t.Run("update hashed field", func(t *testing.T) {

		swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog-hash.yaml")
		if err != nil {
			t.Fatalf("unexpected error occured '%s'", err)
		}
		ctx := context.Background()
		contentType := "Blog"
		schema := swagger.Components.Schemas[contentType].Value
		builder := rest.CreateSchema(ctx, echo.New(), swagger)

		ctx = context.WithValue(ctx, weosContext.USER_ID, "123")

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "cost": "this is a hashed cost", "sha": "sha 123", "sha3": "sha3 123"}
		payload, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}
		existingEntity := &model.ContentEntity{}
		existingEntity, err = existingEntity.FromSchemaAndBuilder(ctx, schema, builder[contentType])
		err = existingEntity.SetValueFromPayload(ctx, payload)
		if err != nil {
			t.Fatalf("unexpected error instantiating content entity '%s'", err)
		}

		if existingEntity.GetString("Title") != "test 1" {
			t.Errorf("expected the title to be '%s', got '%s'", "test 1", existingEntity.GetString("Title"))
		}

		if existingEntity.GetString("Cost") != "this is a hashed cost" {
			t.Errorf("expected the cost to be '%s', got '%s'", "this is a hashed cost", existingEntity.GetString("Cost"))
		}

		updatedBlog := map[string]interface{}{"title": "Updated title", "description": "Updated Description", "url": "www.UpdatedBlog.com", "cost": "updated cost", "sha": "updated sha", "sha3": "updated sha3"}
		updatedPayload, err := json.Marshal(updatedBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		updatedEntity, err := existingEntity.Update(ctx, updatedPayload)
		if err != nil {
			t.Fatalf("unexpected error updating existing entity '%s'", err)
		}

		if updatedEntity.GetString("Title") != "Updated title" {
			t.Errorf("expected the updated title to be '%s', got '%s'", "Updated title", existingEntity.GetString("Title"))
		}

		if updatedEntity.GetString("Description") != "Updated Description" {
			t.Errorf("expected the updated description to be '%s', got '%s'", "Updated Description", existingEntity.GetString("Description"))
		}

		properties := updatedEntity.ToMap()
		base64_Hashed := properties["cost"].(*string)
		sha256_Hashed := properties["sha"].(*string)
		sha3_512_Hashed := properties["sha3"].(*string)

		//BASE64 Check
		decb64, err := b64.URLEncoding.DecodeString(*base64_Hashed)
		if err != nil {
			t.Fatalf("unexpected error decoding hash: %s", err)
		}
		if string(decb64) != "updated cost" {
			t.Fatalf("expected the decoded password to match. Err: %s, Decoded PW:  %s, Plaintext PW: %s", err, decb64, "updated cost")
		}

		//SHA256 Check
		hash := sha256.Sum256([]byte("updated sha"))
		encSHA256 := b64.StdEncoding.EncodeToString(hash[:])

		if encSHA256 != *sha256_Hashed {
			t.Fatalf("expected the encoded password to match. Err: %s, Plain To Enc PW:  %s, Encoded PW: %s", err, encSHA256, *sha256_Hashed)
		}

		//SHA3-512 Check
		hash1 := sha3.Sum512([]byte("updated sha3"))
		encSHA3512 := b64.StdEncoding.EncodeToString(hash1[:])

		if encSHA3512 != *sha3_512_Hashed {
			t.Fatalf("expected the encoded password to match. Err: %s, Plain To Enc PW:  %s, Encoded PW: %s", err, encSHA3512, *sha3_512_Hashed)
		}

	})
}
