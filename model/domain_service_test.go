package model_test

import (
	context3 "context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	api "github.com/wepala/weos/controllers/rest"

	"github.com/getkin/kin-openapi/openapi3"
	context2 "github.com/wepala/weos/context"
	model "github.com/wepala/weos/model"
	"golang.org/x/net/context"
)

func TestDomainService_Create(t *testing.T) {

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}
	mockProjections := &ProjectionMock{
		GetByPropertiesFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]*model.ContentEntity, error) {
			return nil, nil
		},
	}
	//load open api spec
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	contentType = "Blog"
	contentTypeSchema = swagger.Components.Schemas[contentType]
	newContext := context.Background()
	newContext = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
		Name:   contentType,
		Schema: contentTypeSchema.Value,
	})

	t.Run("Testing with valid ID,Title and Description", func(t *testing.T) {
		entityType := "Blog"

		mockBlog := map[string]string{"title": "New Blog", "description": "New Description", "url": "www.NewBlog.com", "lastUpdated": "2106-11-02T15:04:00Z"}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		dService := model.NewDomainService(newContext, mockEventRepository, mockProjections, nil)
		blog, err := dService.Create(newContext, reqBytes, entityType)

		if err != nil {
			t.Fatalf("unexpected error creating content type '%s'", err)
		}
		if blog == nil {
			t.Fatal("expected blog to be returned")
		}
		if blog.GetString("title") != mockBlog["title"] {
			t.Errorf("expected blog title to be %s got %s", mockBlog["title"], blog.GetString("title"))
		}
		if blog.GetString("description") != mockBlog["description"] {
			t.Errorf("expected blog description to be %s got %s", mockBlog["description"], blog.GetString("description"))
		}
		if !strings.EqualFold(blog.GetString("url"), mockBlog["url"]) {
			t.Errorf("expected blog url to be %s got %s", mockBlog["url"], blog.GetString("url"))
		}

		tt, err := time.Parse("2006-01-02T15:04:00Z", mockBlog["lastUpdated"])
		if err != nil {
			t.Fatal(err)
		}
		if blog.GetTime("lastUpdated").String() != model.NewTime(tt).String() {
			t.Errorf("expected blog url to be %s got %s", mockBlog["lastUpdated"], blog.GetString("lastUpdated"))
		}
	})

	t.Run("Testing create with an invalid payload", func(t *testing.T) {
		entityType := "Blog"

		mockBlog := map[string]interface{}{"url": "www.NewBlog.com"}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}
		dService := model.NewDomainService(newContext, mockEventRepository, mockProjections, nil)
		blog, err := dService.Create(newContext, reqBytes, entityType)

		if err.Error() != "entity property title required" {
			t.Fatalf("expected error to be %s got '%s'", "entity property title required", err.Error())
		}
		if blog != nil {
			t.Fatal("expected blog to be nil ")
		}

	})

}

func TestDomainService_CreateBatch(t *testing.T) {

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}
	mockProjections := &ProjectionMock{
		GetByPropertiesFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]*model.ContentEntity, error) {
			return nil, nil
		},
	}

	//load open api spec
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	contentType = "Blog"
	contentTypeSchema = swagger.Components.Schemas[contentType]
	newContext := context.Background()
	newContext = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
		Name:   contentType,
		Schema: contentTypeSchema.Value,
	})
	builder := api.CreateSchema(newContext, echo.New(), swagger)
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, swagger.Components.Schemas[contentType].Value, builder[contentType])

	newContext = context.WithValue(newContext, context2.ENTITY_FACTORY, entityFactory)

	t.Run("Testing with valid ID,Title and Description", func(t *testing.T) {

		entityType := "Blog"

		mockBlogs := [3]map[string]interface{}{
			{"title": "Blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"},
			{"title": "Blog 2", "description": "Description testing 2", "url": "www.TestBlog2.com"},
			{"title": "Blog 3", "description": "Description testing 3", "url": "www.TestBlog3.com"},
		}
		reqBytes, err := json.Marshal(mockBlogs)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		dService := model.NewDomainService(newContext, mockEventRepository, mockProjections, nil)
		blogs, err := dService.CreateBatch(newContext, reqBytes, entityType)

		if err != nil {
			t.Fatalf("unexpected error batch creating content types '%s'", err)
		}

		for i := 0; i < 3; i++ {
			if blogs[i] == nil {
				t.Fatal("expected blog to be returned")
			}
			if blogs[i].GetString("title") != mockBlogs[i]["title"] {
				t.Fatalf("expected there to be generated blog title: %s got %s", mockBlogs[i]["title"], blogs[i].GetString("title"))
			}
			if blogs[i].GetString("url") != mockBlogs[i]["url"] {
				t.Fatalf("expected there to be generated blog Url: %s got %s", mockBlogs[i]["url"], blogs[i].GetString("url"))
			}
		}
	})
}

func TestDomainService_Update(t *testing.T) {
	//load open api spec
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}

	newContext := context.Background()
	entityType := "Blog"
	builder := api.CreateSchema(newContext, echo.New(), swagger)
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(entityType, swagger.Components.Schemas[entityType].Value, builder[entityType])

	newContext = context.WithValue(newContext, context2.ENTITY_FACTORY, entityFactory)
	newContext = context.WithValue(newContext, "id", uint(12))

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": int64(1), "id": uint(12), "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	reqBytes, err := json.Marshal(existingPayload)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}

	existingBlog := &model.ContentEntity{}
	existingBlog, err = existingBlog.FromSchemaWithValues(newContext, swagger.Components.Schemas[entityType].Value, reqBytes)
	if err != nil {
		t.Errorf("unexpected error creating Blog: %s", err)
	}
	existingBlog.SequenceNo = int64(1)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, entityFactory model.EntityFactory, weosID string) (*model.ContentEntity, error) {
			if entityFactory == nil {
				return nil, fmt.Errorf("expected entity factory got nil")
			}
			if weosID == "" {
				return nil, fmt.Errorf("expected weosid got nil")
			}
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
			if entityFactory == nil {
				return nil, fmt.Errorf("expected entity factory got nil")
			}
			if len(identifiers) == 0 {
				return nil, fmt.Errorf("expected identifiers got none")
			}
			return existingBlog, nil
		},
		GetByPropertiesFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]*model.ContentEntity, error) {
			return []*model.ContentEntity{existingBlog}, err
		},
	}

	dService1 := model.NewDomainService(newContext, mockEventRepository, projectionMock, nil)

	t.Run("Testing with valid ID,Title and Description", func(t *testing.T) {

		//Update a blog - payload uses woesID and seq no from the created entity
		updatedPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "title": "Update Blog", "description": "Update Description", "url": "www.Updated!.com"}
		updatedReqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		newContext = context.WithValue(newContext, context2.SEQUENCE_NO, 1)

		reqBytes, err = json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		updatedBlog, err := dService1.Update(newContext, updatedReqBytes, entityType)

		if err != nil {
			t.Fatalf("unexpected error updating content type '%s'", err)
		}
		if updatedBlog == nil {
			t.Fatal("expected blog to be returned")
		}
		if updatedBlog.GetNumber("id") != 12 {
			t.Fatalf("expected blog id to be %d got %f", 12, updatedBlog.GetNumber("id"))
		}
		if updatedBlog.GetString("title") != updatedPayload["title"] {
			t.Fatalf("expected blog title to be %s got %s", updatedPayload["title"], updatedBlog.GetString("title"))
		}
		if updatedBlog.GetString("description") != updatedPayload["description"] {
			t.Fatalf("expected blog description to be %s got %s", updatedPayload["description"], updatedBlog.GetString("description"))
		}
		if updatedBlog.GetString("url") != updatedPayload["url"] {
			t.Fatalf("expected blog url to be %s got %s", updatedPayload["url"], updatedBlog.GetString("url"))
		}
	})

	t.Run("Testing with stale sequence number", func(t *testing.T) {

		//Update a blog - payload uses woesID and seq no from the created entity
		updatedPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "title": "Update Blog", "description": "Update Description", "url": "www.Updated!.com"}
		updatedReqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}
		newContext = context.WithValue(newContext, context2.SEQUENCE_NO, 3)
		reqBytes, err = json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		updatedBlog, err := dService1.Update(newContext, updatedReqBytes, entityType)

		if err == nil {
			t.Fatalf("expected error updating content type '%s'", err)
		}
		if updatedBlog != nil {
			t.Fatal("expected no blog to be returned")
		}
	})

	t.Run("Testing with invalid data", func(t *testing.T) {

		//Update a blog - payload uses woesID and seq no from the created entity
		updatedPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "title": nil, "description": "Update Description", "url": "www.Updated!.com"}
		updatedReqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}
		newContext = context.WithValue(newContext, context2.SEQUENCE_NO, 1)
		reqBytes, err = json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		updatedBlog, err := dService1.Update(newContext, updatedReqBytes, entityType)

		if err == nil {
			t.Fatalf("expected error updating content type '%s'", err)
		}
		if updatedBlog != nil {
			t.Fatal("expected no blog to be returned")
		}
	})
}

func TestDomainService_UpdateCompoundPrimaryKeyID(t *testing.T) {
	t.SkipNow()
	//load open api spec
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog-pk-id.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	contentType = "Blog"
	contentTypeSchema = swagger.Components.Schemas[contentType]
	newContext := context.Background()
	newContext = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
		Name:   contentType,
		Schema: contentTypeSchema.Value,
	})

	newContext1 := newContext

	//Adds primary key ID to context
	newContext = context.WithValue(newContext, "id", "123")

	entityType := "Blog"

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	reqBytes, err := json.Marshal(existingPayload)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}
	newContext = context.WithValue(newContext, context2.SEQUENCE_NO, 1)
	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}

	dService := model.NewDomainService(newContext, mockEventRepository, nil, nil)
	existingBlog, err := dService.Create(newContext, reqBytes, entityType)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, entityFactory model.EntityFactory, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
			return new(model.ContentEntity).Init(context.Background(), reqBytes)
		},
		GetByPropertiesFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]*model.ContentEntity, error) {
			contentEntity, err := new(model.ContentEntity).Init(context.Background(), reqBytes)
			return []*model.ContentEntity{contentEntity}, err
		},
	}

	t.Run("Testing with compound PK - ID", func(t *testing.T) {
		t.Skipped()
		dService1 := model.NewDomainService(newContext, mockEventRepository, projectionMock, nil)

		updatedPayload := map[string]interface{}{"title": "Update Blog", "description": "Update Description", "url": "www.Updated!.com"}
		updatedReqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		updatedBlog, err := dService1.Update(newContext, updatedReqBytes, entityType)

		if err != nil {
			t.Fatalf("unexpected error updating content type '%s'", err)
		}
		if updatedBlog == nil {
			t.Fatal("expected blog to be returned")
		}
		if updatedBlog.GetString("Id") != "123" {
			t.Fatalf("expected blog title to be %s got %s", "123", updatedBlog.GetString("Id"))
		}
		if updatedBlog.GetString("Title") != updatedPayload["title"] {
			t.Fatalf("expected blog title to be %s got %s", updatedPayload["title"], updatedBlog.GetString("Title"))
		}
		if updatedBlog.GetString("Description") != updatedPayload["description"] {
			t.Fatalf("expected blog description to be %s got %s", updatedPayload["description"], updatedBlog.GetString("Description"))
		}
		if updatedBlog.GetString("Url") != updatedPayload["url"] {
			t.Fatalf("expected blog url to be %s got %s", updatedPayload["url"], updatedBlog.GetString("Url"))
		}
	})

	t.Run("Testing without compound PK - ID", func(t *testing.T) {
		dService1 := model.NewDomainService(newContext1, mockEventRepository, projectionMock, nil)

		updatedPayload := map[string]interface{}{"title": "Update Blog", "description": "Update Description", "url": "www.Updated!.com"}
		updatedReqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		updatedBlog, err := dService1.Update(newContext1, updatedReqBytes, entityType)

		if err == nil {
			t.Fatalf("expected error updating content type '%s'", err)
		}
		if updatedBlog != nil {
			t.Fatal("expected blog to not be returned")
		}
	})
}

func TestDomainService_UpdateCompoundPrimaryKeyGuidTitle(t *testing.T) {
	t.SkipNow()
	//load open api spec
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog-pk-guid-title.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	contentType = "Blog"
	contentTypeSchema = swagger.Components.Schemas[contentType]
	newContext := context.Background()
	newContext = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
		Name:   contentType,
		Schema: contentTypeSchema.Value,
	})

	newContext1 := newContext

	//Adds primary key ID to context
	newContext = context.WithValue(newContext, "guid", "123")
	newContext = context.WithValue(newContext, "title", "blog 1")

	entityType := "Blog"

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	reqBytes, err := json.Marshal(existingPayload)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}
	newContext = context.WithValue(newContext, context2.SEQUENCE_NO, 1)

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}

	dService := model.NewDomainService(newContext, mockEventRepository, nil, nil)
	existingBlog, err := dService.Create(newContext, reqBytes, entityType)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, entityFactory model.EntityFactory, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
			return new(model.ContentEntity).Init(context.Background(), reqBytes)
		},
		GetByPropertiesFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]*model.ContentEntity, error) {
			contentEntity, err := new(model.ContentEntity).Init(context.Background(), reqBytes)
			return []*model.ContentEntity{contentEntity}, err
		},
	}

	t.Run("Testing with compound PK - GUID, Title", func(t *testing.T) {
		t.Skipped()
		dService1 := model.NewDomainService(newContext, mockEventRepository, projectionMock, nil)

		updatedPayload := map[string]interface{}{"description": "Update Description", "url": "www.Updated!.com"}
		updatedReqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		reqBytes, err = json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		updatedBlog, err := dService1.Update(newContext, updatedReqBytes, entityType)

		if err != nil {
			t.Fatalf("unexpected error updating content type '%s'", err)
		}
		if updatedBlog == nil {
			t.Fatal("expected blog to be returned")
		}
		if updatedBlog.GetString("Guid") != "123" {
			t.Fatalf("expected blog guid to be %s got %s", "123", updatedBlog.GetString("Guid"))
		}
		if updatedBlog.GetString("Title") != "blog 1" {
			t.Fatalf("expected blog title to be %s got %s", "blog 1", updatedBlog.GetString("Title"))
		}
		if updatedBlog.GetString("Description") != updatedPayload["description"] {
			t.Fatalf("expected blog description to be %s got %s", updatedPayload["description"], updatedBlog.GetString("Description"))
		}
		if updatedBlog.GetString("Url") != updatedPayload["url"] {
			t.Fatalf("expected blog url to be %s got %s", updatedPayload["url"], updatedBlog.GetString("Url"))
		}
	})

	t.Run("Testing without compound PK - GUID, Title", func(t *testing.T) {
		dService1 := model.NewDomainService(newContext1, mockEventRepository, projectionMock, nil)

		updatedPayload := map[string]interface{}{"title": "Update Blog", "description": "Update Description", "url": "www.Updated!.com"}
		updatedReqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		reqBytes, err = json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		updatedBlog, err := dService1.Update(newContext1, updatedReqBytes, entityType)

		if err == nil {
			t.Fatalf("expected error updating content type '%s'", err)
		}
		if updatedBlog != nil {
			t.Fatal("expected blog to not be returned")
		}
	})
}

func TestDomainService_UpdateWithoutIdentifier(t *testing.T) {
	t.Skipped()
	//load open api spec
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	contentType = "Blog"
	contentTypeSchema = swagger.Components.Schemas[contentType]
	newContext := context.Background()
	newContext = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
		Name:   contentType,
		Schema: contentTypeSchema.Value,
	})

	//Adds primary key ID to context
	newContext = context.WithValue(newContext, "id", uint(123))

	entityType := "Blog"

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	reqBytes, err := json.Marshal(existingPayload)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}
	newContext = context.WithValue(newContext, context2.SEQUENCE_NO, 1)

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}

	dService := model.NewDomainService(newContext, mockEventRepository, &ProjectionMock{GetByPropertiesFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]*model.ContentEntity, error) {
		return nil, nil
	}}, nil)
	existingBlog, err := dService.Create(newContext, reqBytes, entityType)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, entityFactory model.EntityFactory, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
			return new(model.ContentEntity).Init(context.Background(), reqBytes)
		},
		GetByPropertiesFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]*model.ContentEntity, error) {
			contentEntity, err := new(model.ContentEntity).Init(context.Background(), reqBytes)
			return []*model.ContentEntity{contentEntity}, err
		},
	}

	t.Run("Testing with compound PK - ID", func(t *testing.T) {
		t.SkipNow()
		dService1 := model.NewDomainService(newContext, mockEventRepository, projectionMock, nil)

		updatedPayload := map[string]interface{}{"title": "Update Blog", "description": "Update Description", "url": "www.Updated!.com"}
		updatedReqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		updatedBlog, err := dService1.Update(newContext, updatedReqBytes, entityType)

		if err != nil {
			t.Fatalf("unexpected error updating content type '%s'", err)
		}
		if updatedBlog == nil {
			t.Fatal("expected blog to be returned")
		}
		if updatedBlog.GetUint("ID") != uint(123) {
			t.Fatalf("expected blog title to be %d got %d", uint(123), updatedBlog.GetUint("ID"))
		}
		if updatedBlog.GetString("Title") != updatedPayload["title"] {
			t.Fatalf("expected blog title to be %s got %s", updatedPayload["title"], updatedBlog.GetString("Title"))
		}
		if updatedBlog.GetString("Description") != updatedPayload["description"] {
			t.Fatalf("expected blog description to be %s got %s", updatedPayload["description"], updatedBlog.GetString("Description"))
		}
		if updatedBlog.GetString("Url") != updatedPayload["url"] {
			t.Fatalf("expected blog url to be %s got %s", updatedPayload["url"], updatedBlog.GetString("Url"))
		}
	})
}

func TestDomainService_Delete(t *testing.T) {
	t.SkipNow()
	//load open api spec
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	contentType = "Blog"
	contentTypeSchema = swagger.Components.Schemas[contentType]
	newContext := context.Background()
	newContext = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
		Name:   contentType,
		Schema: contentTypeSchema.Value,
	})

	newContext = context.WithValue(newContext, "id", uint(12))

	entityType := "Blog"

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": int64(1), "id": uint(12), "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	reqBytes, err := json.Marshal(existingPayload)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}

	dService := model.NewDomainService(newContext, mockEventRepository, nil, nil)
	existingBlog, _ := dService.Create(newContext, reqBytes, entityType)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, entityFactory model.EntityFactory, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
			return new(model.ContentEntity).Init(context.Background(), reqBytes)
		},
		GetByPropertiesFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]*model.ContentEntity, error) {
			contentEntity, err := new(model.ContentEntity).Init(context.Background(), reqBytes)
			return []*model.ContentEntity{contentEntity}, err
		},
	}

	dService1 := model.NewDomainService(newContext, mockEventRepository, projectionMock, nil)

	t.Run("Testing delete with id in path", func(t *testing.T) {

		deletedEntity, err := dService1.Delete(newContext, "", entityType)

		if err != nil {
			t.Fatalf("unexpected error deleting content type '%s'", err)
		}

		if deletedEntity == nil {
			t.Fatalf("unexpected error deleting content type '%s'", err)
		}
	})
	t.Run("Testing delete with entity ID", func(t *testing.T) {
		deletedEntity, err := dService1.Delete(newContext, "dsafdsdfdsf", entityType)

		if err != nil {
			t.Fatalf("unexpected error deleting content type '%s'", err)
		}

		if deletedEntity == nil {
			t.Fatalf("unexpected error deleting content type '%s'", err)
		}
	})
	t.Run("Testing delete with stale item", func(t *testing.T) {
		newContext = context.WithValue(newContext, context2.SEQUENCE_NO, 3)

		deletedEntity, err := dService1.Delete(newContext, "dsafdsdfdsf", entityType)

		if err == nil {
			t.Fatalf("expected error deleting content type '%s'", err)
		}

		if deletedEntity != nil {
			t.Fatalf("expected error deleting content type '%s'", err)
		}
	})
}

func TestDomainService_ValidateUnique(t *testing.T) {

	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	contentType = "Blog"
	contentTypeSchema = swagger.Components.Schemas[contentType]
	newContext := context.Background()
	newContext = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
		Name:   contentType,
		Schema: contentTypeSchema.Value,
	})

	builder := api.CreateSchema(newContext, echo.New(), swagger)
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, swagger.Components.Schemas[contentType].Value, builder[contentType])

	newContext = context.WithValue(newContext, context2.ENTITY_FACTORY, entityFactory)
	newContext = context.WithValue(newContext, "id", uint(12))

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": int64(1), "id": uint(12), "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	reqBytes, err := json.Marshal(existingPayload)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}

	existingPayload2 := map[string]interface{}{"weos_id": "dsafdsdfdsf11", "sequence_no": int64(1), "id": uint(13), "title": "blog 2", "description": "Description testing 2", "url": "www.TestBlog2.com"}
	reqBytes2, err := json.Marshal(existingPayload2)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, entityFactory model.EntityFactory, weosID string) (*model.ContentEntity, error) {
			if weosID == "dsafdsdfdsf" {
				return entityFactory.CreateEntityWithValues(ctx, reqBytes)
			}
			if weosID == "dsafdsdfdsf11" {
				return entityFactory.CreateEntityWithValues(ctx, reqBytes2)
			}
			return nil, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
			if existingPayload["id"] == identifiers["id"] {
				return entityFactory.CreateEntityWithValues(context.Background(), reqBytes)
			}
			if existingPayload2["id"] == identifiers["id"] {
				return entityFactory.CreateEntityWithValues(context.Background(), reqBytes2)
			}
			return nil, nil
		},
		GetByPropertiesFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]*model.ContentEntity, error) {
			identifier := identifiers["url"].(string)
			if identifier == existingPayload["url"].(string) {
				contentEntity, err := new(model.ContentEntity).Init(context.Background(), reqBytes)
				return []*model.ContentEntity{contentEntity}, err
			}
			if identifier == existingPayload2["url"].(string) {
				contentEntity, err := new(model.ContentEntity).Init(context.Background(), reqBytes2)
				return []*model.ContentEntity{contentEntity}, err
			}
			return nil, nil
		},
	}

	dService1 := model.NewDomainService(newContext, mockEventRepository, projectionMock, echo.New().Logger)

	t.Run("Basic unique validation", func(t *testing.T) {
		mockBlog := map[string]interface{}{"weos_id": "09481", "title": "New Blog", "description": "New Description", "url": "www.TestBlog2.com", "last_updated": "2106-11-02T15:04:00Z"}
		newContext1 := context.Background()
		newContext1 = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
			Name:   contentType,
			Schema: contentTypeSchema.Value,
		})
		newContext1 = context.WithValue(newContext1, context2.ENTITY_FACTORY, entityFactory)
		newEntity, err := entityFactory.NewEntity(newContext1)
		if err != nil {
			t.Fatalf("got error creating test fixture %s", err)
		}
		event := model.NewEntityEvent("create", newEntity, newEntity.ID, mockBlog)
		newEntity.NewChange(event)
		err = newEntity.ApplyEvents([]*model.Event{event})
		if err != nil {
			t.Fatalf("got error creating test fixture %s", err)
		}

		err = dService1.ValidateUnique(newContext, newEntity)
		if err == nil {
			t.Fatalf("expected to get an error validing the url")
		}

	})

	t.Run("Create with unique tag", func(t *testing.T) {
		mockBlog := map[string]interface{}{"title": "New Blog", "description": "New Description", "url": "www.TestBlog1.com", "last_updated": "2106-11-02T15:04:00Z"}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}
		_, err = dService1.Create(newContext, reqBytes, contentType)
		if err == nil {
			t.Fatalf("expected a unique entity error to be thrown")
		}
		if !strings.Contains(err.Error(), "unique") {
			t.Fatalf("expected a unique entity error to be thrown")
		}
	})

	t.Run("Update with unique tag", func(t *testing.T) {

		//valid update
		mockBlog := map[string]interface{}{"id": uint(12), "title": "New Blog", "description": "New Description", "url": "www.TestBlog1.com", "last_updated": "2106-11-02T15:04:00Z"}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}
		_, err = dService1.Update(newContext, reqBytes, contentType)
		if err != nil {
			t.Fatalf("expected to be able to update blog, got error %s", err.Error())
		}

		//invalid update

		mockBlog = map[string]interface{}{"title": "New Blog", "description": "New Description", "url": "www.TestBlog1.com", "last_updated": "2106-11-02T15:04:00Z"}
		reqBytes, err = json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}
		newContext = context.WithValue(newContext, "id", uint(13))
		_, err = dService1.Update(newContext, reqBytes, contentType)
		if err == nil {
			t.Fatalf("expected a unique entity error to be thrown")
		}
		if !strings.Contains(err.Error(), "unique") {
			t.Fatalf("expected a unique entity error to be thrown")
		}
	})
}
