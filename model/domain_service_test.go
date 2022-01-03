package model_test

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	context2 "github.com/wepala/weos-service/context"
	model "github.com/wepala/weos-service/model"
	"golang.org/x/net/context"
	"testing"
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
		mockBlog := &Blog{
			Title:       "First blog",
			Description: "Description testing 1",
			Url:         "www.TestBlog.com",
		}
		entityType := "Blog"

		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		dService := model.NewDomainService(newContext, mockEventRepository)
		blog, err := dService.Create(newContext, reqBytes, entityType)

		if err != nil {
			t.Fatalf("unexpected error creating content type '%s'", err)
		}
		if blog == nil {
			t.Fatal("expected blog to be returned")
		}
		if blog.ID == "" {
			t.Fatalf("expected there to be generated blog id: got %s", blog.ID)
		}
		if blog.GetString("Title") != mockBlog.Title {
			t.Fatalf("expected blog title to be %s got %s", mockBlog.Title, blog.GetString("Title"))
		}
		if blog.GetString("Description") != mockBlog.Description {
			t.Fatalf("expected blog description to be %s got %s", mockBlog.Description, blog.GetString("Description"))
		}
		if blog.GetString("Url") != mockBlog.Url {
			t.Fatalf("expected blog url to be %s got %s", mockBlog.Url, blog.GetString("Url"))
		}
	})

	t.Run("Testing with valid ID, Description but no Title (Required field)", func(t *testing.T) {
		mockBlog := &Blog{
			Title:       "",
			Description: "Description testing 2",
			Url:         "www.TestBlog.com",
		}
		entityType := "Blog"

		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		dService := model.NewDomainService(newContext, mockEventRepository)
		blog, err := dService.Create(newContext, reqBytes, entityType)

		if err == nil {
			t.Fatalf("expected error creating content type '%s'", err)
		}
		if blog != nil {
			t.Fatal("expected no blog to be returned")
		}
	})
	t.Run("Testing create with an invalid payload", func(t *testing.T) {
		mockBlog := &TestBlog{
			Url: "ww.testBlog.com",
		}
		entityType := "Blog"
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}
		dService := model.NewDomainService(newContext, mockEventRepository)
		blog, err := dService.Create(newContext, reqBytes, entityType)

		if err == nil {
			t.Fatalf("expected error creating content type '%s'", err)
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
		mockBlogs := &[3]Blog{
			{Title: "Blog 1", Description: "Description testing 1", Url: "www.TestBlog1.com"},
			{Title: "Blog 2", Description: "Description testing 2", Url: "www.TestBlog2.com"},
			{Title: "Blog 3", Description: "Description testing 3", Url: "www.TestBlog3.com"},
		}
		entityType := "Blog"

		reqBytes, err := json.Marshal(mockBlogs)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		dService := model.NewDomainService(newContext, mockEventRepository)
		blogs, err := dService.CreateBatch(newContext, reqBytes, entityType)

		if err != nil {
			t.Fatalf("unexpected error batch creating content types '%s'", err)
		}

		for i := 0; i < 3; i++ {
			if blogs[i] == nil {
				t.Fatal("expected blog to be returned")
			}
			if blogs[i].ID == "" {
				t.Fatalf("expected there to be generated blog id: got %s", blogs[i].ID)
			}
		}
	})
}