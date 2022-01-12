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
	weoscontext "github.com/wepala/weos-service/context"
	"github.com/wepala/weos-service/controllers/rest"
	"github.com/wepala/weos-service/model"
)

type Blog struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Url         string `json:"url"`
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

			blog := &Blog{}
			json.Unmarshal(command.Payload, &blog)

			if blog.Title != mockBlog.Title {
				t.Errorf("expected the blog title to be '%s', got '%s'", mockBlog.Title, blog.Title)
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

	application := &ApplicationMock{
		DispatcherFunc: func() model.Dispatcher {
			return dispatcher
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

		if response.StatusCode != 201 {
			t.Errorf("expected response code to be %d, got %d", 201, response.StatusCode)
		}
	})
}

func TestStandardControllers_CreateBatch(t *testing.T) {
	mockBlog := &[3]Blog{
		{Title: "Blog 1"},
		{Title: "Blog 2"},
		{Title: "Blog 3"},
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

	application := &ApplicationMock{
		DispatcherFunc: func() model.Dispatcher {
			return dispatcher
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

		if len(dispatcher.DispatchCalls()) == 0 {
			t.Error("expected create account command to be dispatched")
		}

		if response.StatusCode != 201 {
			t.Errorf("expected response code to be %d, got %d", 201, response.StatusCode)
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
