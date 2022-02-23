package model_test

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	weosContext "github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
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
	originalName := entity.GetOriginalFieldName("Title")
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
		Title string `json:"title"`
	}{
		Title: "Test Blog",
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

		mockBlog := map[string]interface{}{"title": "test 1", "description": "New Description", "url": "www.NewBlog.com", "status": 0}
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
