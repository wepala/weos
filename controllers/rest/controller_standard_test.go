package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
)

type Blog struct {
	DbID        uint   `json:"id"`
	ID          string `json:"weos_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Url         string `json:"url"`
	SequenceNo  string `json:"sequence_no"`
}

func TestStandardControllers_Create(t *testing.T) {
	mockBlog := &Blog{
		Title: "Test Blog",
	}

	content, err := ioutil.ReadFile("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
	tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
	//replace environment variables in file
	tempFile = os.ExpandEnv(string(tempFile))
	tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	tempFile = re.ReplaceAllString(tempFile, `:$1`)
	content = []byte(tempFile)
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData(content)
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//instantiate api
	e := echo.New()
	restAPI := &rest.RESTAPI{}

	dispatcher := &DispatcherMock{
		DispatchFunc: func(ctx context.Context, command *model.Command) error {

			//if it's a the create blog call let's check to see if the command is what we expect
			if command == nil {
				t.Fatal("no command sent")
			}

			if command.Type != "create" {
				t.Errorf("expected the command to be '%s', got '%s'", "create", command.Type)
			}

			if command.Metadata.EntityType != "Blog" {
				t.Errorf("expected the entity type to be '%s', got '%s'", "Blog", command.Metadata.EntityType)
			}

			if command.Metadata.EntityID == "" {
				t.Errorf("expected the entity ID to be generated, got '%s'", command.Metadata.EntityID)
			}

			blog := &TestBlog{}
			json.Unmarshal(command.Payload, &blog)

			if blog.Title == nil {
				return model.NewDomainError("expected the blog title to be a title got nil", command.Metadata.EntityType, "", nil)
			}
			if blog.Url == nil {
				return model.NewDomainError("expected a blog url but got nil", command.Metadata.EntityType, "", nil)
			}
			//check that content type information is in the context
			contentType := weoscontext.GetContentType(ctx)
			if contentType == nil {
				t.Fatal("expected a content type to be in the context")
			}

			if contentType.Name != "Blog" {
				t.Errorf("expected the content type to be'%s', got %s", "Blog", contentType.Name)
			}

			if _, ok := contentType.Schema.Properties["title"]; !ok {
				t.Errorf("expected a property '%s' on content type '%s'", "title", "blog")
			}

			return nil
		},
	}

	mockPayload := map[string]interface{}{"weos_id": "123456", "sequence_no": int64(1), "title": "Test Blog", "description": "testing"}
	mockContentEntity := &model.ContentEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{
				ID: "123456",
			},
			SequenceNo: 1,
		},
		Property: mockPayload,
	}

	projections := &ProjectionMock{
		GetContentEntityFunc: func(ctx context.Context, weosID string) (*model.ContentEntity, error) {
			return mockContentEntity, nil
		},
	}

	application := &ApplicationMock{
		DispatcherFunc: func() model.Dispatcher {
			return dispatcher
		},
		ProjectionsFunc: func() []model.Projection {
			return []model.Projection{projections}
		},
	}

	//initialization will instantiate with application so we need to overwrite with our mock application
	restAPI.Application = application

	t.Run("basic create based on simple content type", func(t *testing.T) {
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error setting up request %s", err)
		}
		body := bytes.NewReader(reqBytes)

		accountID := "Create Blog"
		path := swagger.Paths.Find("/blogs")
		controller := restAPI.Create(restAPI.Application, swagger, path, path.Post)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set(weoscontext.HeaderXAccountID, accountID)
		mw := rest.Context(restAPI.Application, swagger, path, path.Post)
		e.POST("/blogs", controller, mw)
		e.ServeHTTP(resp, req)

		response := resp.Result()
		defer response.Body.Close()

		if len(dispatcher.DispatchCalls()) == 0 {
			t.Error("expected create account command to be dispatched")
		}

		if response.Header.Get("Etag") != "123456.1" {
			t.Errorf("expected an Etag, got %s", response.Header.Get("Etag"))
		}

		if response.StatusCode != 201 {
			t.Errorf("expected response code to be %d, got %d", 201, response.StatusCode)
		}
	})
	t.Run("create payload without a required field", func(t *testing.T) {
		title := "Test Blog"
		mockBlog1 := &TestBlog{
			Title: &title,
		}
		reqBytes, err := json.Marshal(mockBlog1)
		if err != nil {
			t.Fatalf("error setting up request %s", err)
		}
		body := bytes.NewReader(reqBytes)

		accountID := "Create Blog"
		path := swagger.Paths.Find("/blogs")
		controller := restAPI.Create(restAPI.Application, swagger, path, path.Post)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set(weoscontext.HeaderXAccountID, accountID)
		mw := rest.Context(restAPI.Application, swagger, path, path.Post)
		e.POST("/blogs", controller, mw)
		e.ServeHTTP(resp, req)

		response := resp.Result()
		defer response.Body.Close()

		if len(dispatcher.DispatchCalls()) == 0 {
			t.Error("expected create account command to be dispatched")
		}

		if response.StatusCode != 400 {
			t.Errorf("expected response code to be %d, got %d", 400, response.StatusCode)
		}
	})
}

func TestStandardControllers_CreateBatch(t *testing.T) {
	mockBlog := &[3]Blog{
		{Title: "Blog 1", Url: "www.Test.com"},
		{Title: "Blog 2", Url: "www.Test.com"},
		{Title: "Blog 3", Url: "www.Test.com"},
	}

	content, err := ioutil.ReadFile("./fixtures/blog-create-batch.yaml")
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
	tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
	//replace environment variables in file
	tempFile = os.ExpandEnv(string(tempFile))
	tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	tempFile = re.ReplaceAllString(tempFile, `:$1`)
	content = []byte(tempFile)
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData(content)
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//instantiate api
	e := echo.New()
	restAPI := &rest.RESTAPI{}

	dispatcher := &DispatcherMock{
		DispatchFunc: func(ctx context.Context, command *model.Command) error {
			accountID := weoscontext.GetAccount(ctx)
			//if it's a the create blog call let's check to see if the command is what we expect
			if accountID == "Create Blog" {
				if command == nil {
					t.Fatal("no command sent")
				}

				if command.Type != "create_batch" {
					t.Errorf("expected the command to be '%s', got '%s'", "create", command.Type)
				}

				if command.Metadata.EntityType != "Blog" {
					t.Errorf("expected the entity type to be '%s', got '%s'", "Blog", command.Metadata.EntityType)
				}

				blog := &[3]Blog{}
				json.Unmarshal(command.Payload, &blog)

				if blog[0].Title != mockBlog[0].Title {
					t.Errorf("expected the blog 1 title to be '%s', got '%s'", mockBlog[0].Title, blog[0].Title)
				}
				if blog[1].Title != mockBlog[1].Title {
					t.Errorf("expected the blog 2 title to be '%s', got '%s'", mockBlog[1].Title, blog[1].Title)
				}
				if blog[2].Title != mockBlog[2].Title {
					t.Errorf("expected the blog 3 title to be '%s', got '%s'", mockBlog[2].Title, blog[2].Title)
				}

			}
			return nil
		},
	}

	projection := &ProjectionMock{
		GetByKeyFunc: func(ctxt context.Context, contentType weoscontext.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
			return nil, nil
		},
		GetByEntityIDFunc: func(ctxt context.Context, contentType weoscontext.ContentType, id string) (map[string]interface{}, error) {
			return nil, nil
		},
	}

	application := &ApplicationMock{
		DispatcherFunc: func() model.Dispatcher {
			return dispatcher
		},
		ProjectionsFunc: func() []model.Projection {
			return []model.Projection{projection}
		},
	}

	//initialization will instantiate with application so we need to overwrite with our mock application
	restAPI.Application = application

	t.Run("basic batch create based on simple content type", func(t *testing.T) {
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error setting up request %s", err)
		}
		body := bytes.NewReader(reqBytes)

		accountID := "Create Blog"
		path := swagger.Paths.Find("/blogs")
		controller := restAPI.CreateBatch(restAPI.Application, swagger, path, path.Post)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set(weoscontext.HeaderXAccountID, accountID)
		mw := rest.Context(restAPI.Application, swagger, path, path.Post)
		e.POST("/blogs", controller, mw)
		e.ServeHTTP(resp, req)

		response := resp.Result()
		defer response.Body.Close()

		if response.StatusCode != 201 {
			t.Errorf("expected response code to be %d, got %d", 201, response.StatusCode)
		}
	})
}

func TestStandardControllers_HealthCheck(t *testing.T) {

	content, err := ioutil.ReadFile("./fixtures/blog-create-batch.yaml")
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
	tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
	//replace environment variables in file
	tempFile = os.ExpandEnv(string(tempFile))
	tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	tempFile = re.ReplaceAllString(tempFile, `:$1`)
	content = []byte(tempFile)
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData(content)
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//instantiate api
	e := echo.New()
	restAPI := &rest.RESTAPI{}

	path := swagger.Paths.Find("/health")
	controller := restAPI.HealthCheck(restAPI.Application, swagger, path, path.Get)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	mw := rest.Context(restAPI.Application, swagger, path, path.Get)
	e.GET("/health", controller, mw)
	e.ServeHTTP(resp, req)
	response := resp.Result()
	defer response.Body.Close()
	//check response code
	if response.StatusCode != 200 {
		t.Errorf("expected response code to be %d, got %d", 200, response.StatusCode)
	}

}

func TestStandardControllers_Update(t *testing.T) {
	weosId := "123"
	mockBlog := &Blog{
		Title:       "Test Blog",
		Description: "testing description",
	}
	mockBlog1 := &Blog{
		Title:       "Test changing Blog",
		Description: "testing changing description",
	}

	content, err := ioutil.ReadFile("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
	tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
	//replace environment variables in file
	tempFile = os.ExpandEnv(string(tempFile))
	tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	tempFile = re.ReplaceAllString(tempFile, `:$1`)
	content = []byte(tempFile)
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData(content)
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//instantiate api
	e := echo.New()
	restAPI := &rest.RESTAPI{}

	dispatcher := &DispatcherMock{
		DispatchFunc: func(ctx context.Context, command *model.Command) error {

			//if it's a the update blog call let's check to see if the command is what we expect
			if command == nil {
				t.Fatal("no command sent")
			}

			if command.Type != "update" {
				t.Errorf("expected the command to be '%s', got '%s'", "update", command.Type)
			}

			if command.Metadata.EntityType != "Blog" {
				t.Errorf("expected the entity type to be '%s', got '%s'", "Blog", command.Metadata.EntityType)
			}

			blog := &Blog{}
			json.Unmarshal(command.Payload, &blog)

			if blog.Title != mockBlog1.Title {
				t.Errorf("expected the blog title to be '%s', got '%s'", mockBlog1.Title, blog.Title)
			}

			if ctx.Value(weoscontext.WEOS_ID).(string) != weosId {
				t.Errorf("expected the blog weos id to be '%s', got '%s'", weosId, blog.ID)
			}

			//check that content type information is in the context
			contentType := weoscontext.GetContentType(ctx)
			if contentType == nil {
				t.Fatal("expected a content type to be in the context")
			}

			if contentType.Name != "Blog" {
				t.Errorf("expected the content type to be'%s', got %s", "Blog", contentType.Name)
			}

			if _, ok := contentType.Schema.Properties["title"]; !ok {
				t.Errorf("expected a property '%s' on content type '%s'", "title", "blog")
			}

			if _, ok := contentType.Schema.Properties["description"]; !ok {
				t.Errorf("expected a property '%s' on content type '%s'", "description", "blog")
			}

			id := ctx.Value("id").(string)
			if id != weosId {
				t.Errorf("unexpected error, expected id to be %s got %s", weosId, id)
			}

			etag := ctx.Value("If-Match").(string)
			if etag != "123.1" {
				t.Errorf("unexpected error, expected etag to be %s got %s", "123.1", etag)
			}

			return nil
		},
	}
	mockEntity := &model.ContentEntity{}
	mockEntity.ID = weosId
	mockEntity.SequenceNo = int64(1)
	mockEntity.Property = mockBlog

	projection := &ProjectionMock{
		GetByKeyFunc: func(ctxt context.Context, contentType weoscontext.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
			return nil, nil
		},
		GetByEntityIDFunc: func(ctxt context.Context, contentType weoscontext.ContentType, id string) (map[string]interface{}, error) {
			return nil, nil
		},
		GetContentEntityFunc: func(ctx context.Context, weosID string) (*model.ContentEntity, error) {
			return mockEntity, nil
		},
	}

	application := &ApplicationMock{
		DispatcherFunc: func() model.Dispatcher {
			return dispatcher
		},
		ProjectionsFunc: func() []model.Projection {
			return []model.Projection{projection}
		},
	}

	//initialization will instantiate with application so we need to overwrite with our mock application
	restAPI.Application = application

	t.Run("basic update based on simple content type with id parameter in path and etag", func(t *testing.T) {
		paramName := "id"
		reqBytes, err := json.Marshal(mockBlog1)
		if err != nil {
			t.Fatalf("error setting up request %s", err)
		}
		body := bytes.NewReader(reqBytes)

		accountID := "Update Blog"
		path := swagger.Paths.Find("/blogs/:" + paramName)
		controller := restAPI.Update(restAPI.Application, swagger, path, path.Put)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/blogs/"+weosId, body)
		req.Header.Set(weoscontext.HeaderXAccountID, accountID)
		req.Header.Set("If-Match", weosId+".1")
		mw := rest.Context(restAPI.Application, swagger, path, path.Put)
		e.PUT("/blogs/:"+paramName, controller, mw)
		e.ServeHTTP(resp, req)

		response := resp.Result()
		defer response.Body.Close()

		if response.StatusCode != 200 {
			t.Errorf("expected response code to be %d, got %d", 200, response.StatusCode)
		}
	})
}

func TestStandardControllers_View(t *testing.T) {
	content, err := ioutil.ReadFile("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
	tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
	//replace environment variables in file
	tempFile = os.ExpandEnv(string(tempFile))
	tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	tempFile = re.ReplaceAllString(tempFile, `:$1`)
	content = []byte(tempFile)
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData(content)
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//instantiate api
	e := echo.New()
	restAPI := &rest.RESTAPI{}

	dispatcher := &DispatcherMock{
		DispatchFunc: func(ctx context.Context, command *model.Command) error {
			return nil
		},
	}

	projection := &ProjectionMock{
		GetByKeyFunc: func(ctxt context.Context, contentType weoscontext.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{
				"id":      "1234sd",
				"weos_id": "1234sd",
			}, nil
		},
		GetByEntityIDFunc: func(ctxt context.Context, contentType weoscontext.ContentType, id string) (map[string]interface{}, error) {
			return map[string]interface{}{
				"id":      "1234sd",
				"weos_id": "1234sd",
			}, nil
		},
	}

	application := &ApplicationMock{
		DispatcherFunc: func() model.Dispatcher {
			return dispatcher
		},
		ProjectionsFunc: func() []model.Projection {
			return []model.Projection{projection}
		},
	}

	//initialization will instantiate with application so we need to overwrite with our mock application
	restAPI.Application = application

	t.Run("Testing the generic list endpoint", func(t *testing.T) {
		paramName := "id"
		paramValue := "1234sd"
		path := swagger.Paths.Find("/blogs/:" + paramName)
		controller := restAPI.View(restAPI.Application, swagger, path, path.Get)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs/"+paramValue, nil)
		mw := rest.Context(restAPI.Application, swagger, path, path.Get)
		e.GET("/blogs/:"+paramName, controller, mw)
		e.ServeHTTP(resp, req)

		response := resp.Result()
		defer response.Body.Close()

		if response.StatusCode != 200 {
			t.Errorf("expected response code to be %d, got %d", 200, response.StatusCode)
		}
	})
}

func TestStandardControllers_List(t *testing.T) {
	content, err := ioutil.ReadFile("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
	tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
	//replace environment variables in file
	tempFile = os.ExpandEnv(string(tempFile))
	tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	tempFile = re.ReplaceAllString(tempFile, `:$1`)
	content = []byte(tempFile)
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData(content)
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//instantiate api
	e := echo.New()
	restAPI := &rest.RESTAPI{}

	dispatcher := &DispatcherMock{
		DispatchFunc: func(ctx context.Context, command *model.Command) error {
			return nil
		},
	}

	application := &ApplicationMock{
		DispatcherFunc: func() model.Dispatcher {
			return dispatcher
		},
	}

	//initialization will instantiate with application so we need to overwrite with our mock application
	restAPI.Application = application

	t.Run("Testing the generic list endpoint", func(t *testing.T) {
		path := swagger.Paths.Find("/blogs")
		controller := restAPI.List(restAPI.Application, swagger, path, path.Get)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs", nil)
		mw := rest.Context(restAPI.Application, swagger, path, path.Get)
		e.GET("/blogs", controller, mw)
		e.ServeHTTP(resp, req)

		response := resp.Result()
		defer response.Body.Close()

		if response.StatusCode != 200 {
			t.Errorf("expected response code to be %d, got %d", 200, response.StatusCode)
		}
	})
}
