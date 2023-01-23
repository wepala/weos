package rest_test

import (
	context3 "context"
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultWriteController(t *testing.T) {
	swagger, err := LoadConfig(t, "./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error loading api config '%s'", err)
	}

	t.Run("test create", func(t *testing.T) {
		container := &ContainerMock{
			GetEventStoreFunc: func(name string) (model.EventRepository, error) {
				return &EventRepositoryMock{}, nil
			},
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
		}
		commandDispatcher := &CommandDispatcherMock{
			DispatchFunc: func(ctx context3.Context, command *model.Command, container model.Container, repository model.EntityRepository, logger model.Log) (interface{}, error) {
				if command.Type != model.CREATE_COMMAND {
					t.Errorf("expected command type '%s', got '%s'", model.CREATE_COMMAND, command.Type)
				}
				return nil, nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
		}

		path := swagger.Paths.Find("/blogs")

		controller := rest.DefaultWriteController(container, commandDispatcher, repository, nil, map[string]*openapi3.Operation{
			http.MethodPost: path.Post,
		})
		e := echo.New()
		e.POST("/blogs", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.POST, "/blogs", strings.NewReader(`{"title":"test"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusCreated {
			t.Errorf("expected status code %d, got %d", http.StatusCreated, resp.Code)
		}

		//TODO check that the response body is correct
	})

	t.Run("test update", func(t *testing.T) {
		container := &ContainerMock{
			GetEventStoreFunc: func(name string) (model.EventRepository, error) {
				return &EventRepositoryMock{}, nil
			},
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
		}
		commandDispatcher := &CommandDispatcherMock{
			DispatchFunc: func(ctx context3.Context, command *model.Command, container model.Container, repository model.EntityRepository, logger model.Log) (interface{}, error) {
				if command.Type != model.UPDATE_COMMAND {
					t.Errorf("expected command type '%s', got '%s'", model.UPDATE_COMMAND, command.Type)
				}
				return nil, nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
		}

		path := swagger.Paths.Find("/blogs/:id")

		controller := rest.DefaultWriteController(container, commandDispatcher, repository, nil, map[string]*openapi3.Operation{
			http.MethodPut: path.Put,
		})
		e := echo.New()
		e.PUT("/blogs/1", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.PUT, "/blogs/1", strings.NewReader(`{"title":"test"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("expected status code %d, got %d", http.StatusOK, resp.Code)
		}
	})

	t.Run("test delete", func(t *testing.T) {
		container := &ContainerMock{
			GetEventStoreFunc: func(name string) (model.EventRepository, error) {
				return &EventRepositoryMock{}, nil
			},
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
		}
		commandDispatcher := &CommandDispatcherMock{
			DispatchFunc: func(ctx context3.Context, command *model.Command, container model.Container, repository model.EntityRepository, logger model.Log) (interface{}, error) {
				if command.Type != model.DELETE_COMMAND {
					t.Errorf("expected command type '%s', got '%s'", model.CREATE_COMMAND, command.Type)
				}
				return nil, nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
		}

		path := swagger.Paths.Find("/blogs/:id")

		controller := rest.DefaultWriteController(container, commandDispatcher, repository, nil, map[string]*openapi3.Operation{
			http.MethodDelete: path.Delete,
		})
		e := echo.New()
		e.DELETE("/blogs/:id", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.DELETE, "/blogs/1", nil)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("expected status code %d, got %d", http.StatusOK, resp.Code)
		}
	})

	t.Run("test custom command", func(t *testing.T) {

	})
}

func TestDefaultReadController(t *testing.T) {
	swagger, err := LoadConfig(t, "./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error loading api config '%s'", err)
	}

	t.Run("test get item", func(t *testing.T) {
		container := &ContainerMock{
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return new(model.ContentEntity).FromSchemaWithValues(ctxt, swagger.Components.Schemas["Blog"].Value, []byte(`{"id":1,"title":"test"}`))
			},
			CreateEntityWithValuesFunc: func(ctx context3.Context, payload []byte) (*model.ContentEntity, error) {
				return new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Blog"].Value, []byte(`{}`))
			},
		}

		path := swagger.Paths.Find("/blogs/:id")

		controller := rest.DefaultReadController(container, &CommandDispatcherMock{}, repository, map[string]*openapi3.PathItem{
			"/blogs/1": path,
		}, map[string]*openapi3.Operation{
			http.MethodGet: path.Get,
		})
		e := echo.New()
		e.GET("/blogs/:id", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/blogs/1", nil)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Add(echo.HeaderAccept, echo.MIMEApplicationJSON)
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("expected status code %d, got %d", http.StatusOK, resp.Code)
		}

		if len(repository.GetByKeyCalls()) != 1 {
			t.Errorf("expected repository.GetByKey to be called once, got %d", len(repository.GetByKeyCalls()))
		}

		if len(resp.Body.String()) == 0 {
			t.Errorf("expected body to be not empty")
		}

		var blog map[string]interface{}
		if err := json.Unmarshal(resp.Body.Bytes(), &blog); err != nil {
			t.Errorf("error unmarshalling body: %s", err)
		}
		//check that the response body is correct
		if blog["id"] != float64(1) {
			t.Errorf("expected id to be 1, got %d", blog["id"])
		}

		if blog["title"] != "test" {
			t.Errorf("expected title to be 'test', got '%s'", blog["title"])
		}
	})

	t.Run("test get list of items", func(t *testing.T) {
		container := &ContainerMock{
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return nil, nil
			},
			CreateEntityWithValuesFunc: func(ctx context3.Context, payload []byte) (*model.ContentEntity, error) {
				return new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Blog"].Value, []byte(`{}`))
			},
			SchemaFunc: func() *openapi3.Schema {
				return swagger.Components.Schemas["Blog"].Value
			},
			GetListFunc: func(ctx context3.Context, entityFactory model.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]*model.ContentEntity, int64, error) {
				entity, _ := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Blog"].Value, []byte(`{"id":1,"title":"test"}`))
				return []*model.ContentEntity{entity}, 1, nil
			},
		}

		path := swagger.Paths.Find("/blogs")

		controller := rest.DefaultListController(container, &CommandDispatcherMock{}, repository, map[string]*openapi3.PathItem{
			"/blogs": path,
		}, map[string]*openapi3.Operation{
			http.MethodGet: path.Get,
		})
		e := echo.New()
		e.GET("/blogs", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/blogs", nil)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("expected status code %d, got %d", http.StatusOK, resp.Code)
		}

		if len(repository.GetListCalls()) != 1 {
			t.Errorf("expected repository.GetList to be called once, got %d", len(repository.GetListCalls()))
		}

		if len(resp.Body.String()) == 0 {
			t.Errorf("expected body to be not empty")
		}

		//TODO check that the response body is correct
	})

	t.Run("test render static html with multiple templates", func(t *testing.T) {
		container := &ContainerMock{
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return nil, nil
			},
			CreateEntityWithValuesFunc: func(ctx context3.Context, payload []byte) (*model.ContentEntity, error) {
				return new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Blog"].Value, []byte(`{}`))
			},
		}

		path := swagger.Paths.Find("/multipletemplates")

		controller := rest.DefaultReadController(container, &CommandDispatcherMock{}, repository, map[string]*openapi3.PathItem{
			"/multipletemplates": path,
		}, map[string]*openapi3.Operation{
			http.MethodGet: path.Get,
		})
		e := echo.New()
		e.GET("/multipletemplates", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/multipletemplates", nil)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		e.ServeHTTP(resp, req)
		response := resp.Result()
		defer response.Body.Close()
		expectResp := "<html>\n    <body>\n        <h1>About</h1>\n\n\n        \n<p>About us page now</p>\n\n    </body>\n</html>"

		if resp.Code != http.StatusOK {
			t.Errorf("expected status code %d, got %d", http.StatusOK, resp.Code)
		}
		results, err := io.ReadAll(response.Body)
		if err != nil {
			t.Errorf("unexpected error reading the response body: %s", err)
		}
		if !strings.Contains(expectResp, string(results)) {
			t.Errorf("expected results to be %s got %s", expectResp, string(results))
		}
	})

	t.Run("test get item that's not found", func(t *testing.T) {
		container := &ContainerMock{
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return nil, nil
			},
			CreateEntityWithValuesFunc: func(ctx context3.Context, payload []byte) (*model.ContentEntity, error) {
				return new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Blog"].Value, []byte(`{}`))
			},
		}

		path := swagger.Paths.Find("/blogs/:id")

		controller := rest.DefaultReadController(container, &CommandDispatcherMock{}, repository, map[string]*openapi3.PathItem{
			"/blogs/1": path,
		}, map[string]*openapi3.Operation{
			http.MethodGet: path.Get,
		})
		e := echo.New()
		e.GET("/blogs/:id", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/blogs/1", nil)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Add(echo.HeaderAccept, echo.MIMEApplicationJSON)
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusNotFound {
			t.Errorf("expected status code %d, got %d", http.StatusNotFound, resp.Code)
		}

		if len(repository.GetByKeyCalls()) != 1 {
			t.Errorf("expected repository.GetByKey to be called once, got %d", len(repository.GetByKeyCalls()))
		}

		body := resp.Body.String()

		if strings.Contains("null", body) {
			t.Errorf("expected body to be empty")
		}
	})
}
