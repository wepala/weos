package model_test

import (
	context3 "context"
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	context2 "github.com/wepala/weos-service/context"
	model "github.com/wepala/weos-service/model"
	"golang.org/x/net/context"
)

func TestDomainService_Create(t *testing.T) {

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
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

		mockBlog := map[string]interface{}{"title": "New Blog", "description": "New Description", "url": "www.NewBlog.com"}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		dService := model.NewDomainService(newContext, mockEventRepository, nil)
		blog, err := dService.Create(newContext, reqBytes, entityType)

		if err != nil {
			t.Fatalf("unexpected error creating content type '%s'", err)
		}
		if blog == nil {
			t.Fatal("expected blog to be returned")
		}
		if blog.GetString("Title") != mockBlog["title"] {
			t.Fatalf("expected blog title to be %s got %s", mockBlog["title"], blog.GetString("Title"))
		}
		if blog.GetString("Description") != mockBlog["description"] {
			t.Fatalf("expected blog description to be %s got %s", mockBlog["description"], blog.GetString("Description"))
		}
		if blog.GetString("Url") != mockBlog["url"] {
			t.Fatalf("expected blog url to be %s got %s", mockBlog["url"], blog.GetString("Url"))
		}
	})

	t.Run("Testing create with an invalid payload", func(t *testing.T) {
		entityType := "Blog"

		mockBlog := map[string]interface{}{"url": "www.NewBlog.com"}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}
		dService := model.NewDomainService(newContext, mockEventRepository, nil)
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

		mockBlogs := [3]map[string]interface{}{
			{"title": "Blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"},
			{"title": "Blog 2", "description": "Description testing 2", "url": "www.TestBlog2.com"},
			{"title": "Blog 3", "description": "Description testing 3", "url": "www.TestBlog3.com"},
		}
		reqBytes, err := json.Marshal(mockBlogs)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		dService := model.NewDomainService(newContext, mockEventRepository, nil)
		blogs, err := dService.CreateBatch(newContext, reqBytes, entityType)

		if err != nil {
			t.Fatalf("unexpected error batch creating content types '%s'", err)
		}

		for i := 0; i < 3; i++ {
			if blogs[i] == nil {
				t.Fatal("expected blog to be returned")
			}
			if blogs[i].GetString("Title") != mockBlogs[i]["title"] {
				t.Fatalf("expected there to be generated blog title: %s got %s", mockBlogs[i]["title"], blogs[i].GetString("Title"))
			}
			if blogs[i].GetString("Url") != mockBlogs[i]["url"] {
				t.Fatalf("expected there to be generated blog Url: %s got %s", mockBlogs[i]["url"], blogs[i].GetString("Url"))
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

	dService := model.NewDomainService(newContext, mockEventRepository, nil)
	existingBlog, _ := dService.Create(newContext, reqBytes, entityType)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, contentType context2.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
			return existingPayload, nil
		},
	}

	dService1 := model.NewDomainService(newContext, mockEventRepository, projectionMock)

	t.Run("Testing with valid ID,Title and Description", func(t *testing.T) {

		//Update a blog - payload uses woesID and seq no from the created entity
		updatedPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": "1", "title": "Update Blog", "description": "Update Description", "url": "www.Updated!.com"}
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
		if updatedBlog.GetUint("ID") != uint(12) {
			t.Fatalf("expected blog id to be %d got %d", uint(12), updatedBlog.GetUint("id"))
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

	t.Run("Testing with stale sequence number", func(t *testing.T) {

		//Update a blog - payload uses woesID and seq no from the created entity
		updatedPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": "3", "title": "Update Blog", "description": "Update Description", "url": "www.Updated!.com"}
		updatedReqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

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
		updatedPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": "1", "title": nil, "description": "Update Description", "url": "www.Updated!.com"}
		updatedReqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

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

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": int64(1), "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	reqBytes, err := json.Marshal(existingPayload)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}

	dService := model.NewDomainService(newContext, mockEventRepository, nil)
	existingBlog, err := dService.Create(newContext, reqBytes, entityType)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, contentType context2.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
			return existingPayload, nil
		},
	}

	t.Run("Testing with compound PK - ID", func(t *testing.T) {
		dService1 := model.NewDomainService(newContext, mockEventRepository, projectionMock)

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
		dService1 := model.NewDomainService(newContext1, mockEventRepository, projectionMock)

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

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": int64(1), "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	reqBytes, err := json.Marshal(existingPayload)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}

	dService := model.NewDomainService(newContext, mockEventRepository, nil)
	existingBlog, err := dService.Create(newContext, reqBytes, entityType)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, contentType context2.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
			return existingPayload, nil
		},
	}

	t.Run("Testing with compound PK - GUID, Title", func(t *testing.T) {

		dService1 := model.NewDomainService(newContext, mockEventRepository, projectionMock)

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
		dService1 := model.NewDomainService(newContext1, mockEventRepository, projectionMock)

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
	newContext = context.WithValue(newContext, "id", "123")

	entityType := "Blog"

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": int64(1), "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	reqBytes, err := json.Marshal(existingPayload)
	if err != nil {
		t.Fatalf("error converting payload to bytes %s", err)
	}

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}

	dService := model.NewDomainService(newContext, mockEventRepository, nil)
	existingBlog, err := dService.Create(newContext, reqBytes, entityType)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, contentType context2.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
			return existingPayload, nil
		},
	}

	t.Run("Testing with compound PK - ID", func(t *testing.T) {
		dService1 := model.NewDomainService(newContext, mockEventRepository, projectionMock)

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
