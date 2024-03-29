package rest_test

import (
	context3 "context"
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"io"
	"io/ioutil"
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

	t.Run("test get item with no accept header", func(t *testing.T) {
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
}

func TestDefaultListController(t *testing.T) {
	swagger, err := LoadConfig(t, "./fixtures/csv.yaml")
	if err != nil {
		t.Fatalf("error loading api config '%s'", err)
	}

	t.Run("test _format & _headers parameters", func(t *testing.T) {
		container := &ContainerMock{
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
			GetWeOSConfigFunc: func() *rest.APIConfig {
				return nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Customer"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return nil, nil
			},
			CreateEntityWithValuesFunc: func(ctx context3.Context, payload []byte) (*model.ContentEntity, error) {
				return new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Customer"].Value, []byte(`{}`))
			},
			SchemaFunc: func() *openapi3.Schema {
				return swagger.Components.Schemas["Customer"].Value
			},
			GetListFunc: func(ctx context3.Context, entityFactory model.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]*model.ContentEntity, int64, error) {
				entity1, _ := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Customer"].Value, []byte(`{"id":123,"firstName":"John", "lastName":"Doe"}`))
				entity2, _ := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Customer"].Value, []byte(`{"id":123,"firstName":"Jane", "lastName":"Doe"}`))
				var entities []*model.ContentEntity
				entities = append(entities, entity1)
				entities = append(entities, entity2)
				return entities, 2, nil
			},
		}

		path := swagger.Paths.Find("/customers")

		mw := rest.Context(container, &CommandDispatcherMock{}, repository, path, path.Get)

		controller := mw(rest.DefaultListController(container, &CommandDispatcherMock{}, repository, map[string]*openapi3.PathItem{
			"/customers": path,
		}, map[string]*openapi3.Operation{
			http.MethodGet: path.Get,
		}))
		e := echo.New()
		e.GET("/customers", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/customers?_format=text/csv&_headers[firstName]=First+Name&_headers[lastName]=Last+Name", nil)
		req.Header.Set(echo.HeaderContentType, "text/csv")
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

		if resp.Header().Get("Content-Type") != "text/csv" {
			t.Errorf("expected content type to be %s got %s", "text/csv", resp.Header().Get("Content-Type"))
		}

		if !strings.Contains(resp.Header().Get("Content-Disposition"), repository.Name()) {
			t.Errorf("expected Content-Disposition header to contain  %s got %s", repository.Name(), resp.Header().Get("Content-Disposition"))
		}

		// check contents of csv
		data, err := ioutil.ReadFile("./fixtures/customers.csv")
		if err != nil {
			t.Errorf("error reading customers.csv")
		}
		expected := string(data)
		results := resp.Body.String()

		if expected != results {
			t.Errorf("expected response body to be %s got %s", expected, results)
		}

	})

	t.Run("test _format with no _headers parameters", func(t *testing.T) {
		container := &ContainerMock{
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
			GetWeOSConfigFunc: func() *rest.APIConfig {
				return nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Customer"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return nil, nil
			},
			CreateEntityWithValuesFunc: func(ctx context3.Context, payload []byte) (*model.ContentEntity, error) {
				return new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Customer"].Value, []byte(`{}`))
			},
			SchemaFunc: func() *openapi3.Schema {
				return swagger.Components.Schemas["Customer"].Value
			},
			GetListFunc: func(ctx context3.Context, entityFactory model.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]*model.ContentEntity, int64, error) {
				entity1, _ := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Customer"].Value, []byte(`{"id":123,"firstName":"John", "lastName":"Doe"}`))
				entity2, _ := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Customer"].Value, []byte(`{"id":123,"firstName":"Jane", "lastName":"Doe"}`))
				var entities []*model.ContentEntity
				entities = append(entities, entity1)
				entities = append(entities, entity2)
				return entities, 2, nil
			},
		}

		path := swagger.Paths.Find("/customers")

		mw := rest.Context(container, &CommandDispatcherMock{}, repository, path, path.Get)

		controller := mw(rest.DefaultListController(container, &CommandDispatcherMock{}, repository, map[string]*openapi3.PathItem{
			"/customers": path,
		}, map[string]*openapi3.Operation{
			http.MethodGet: path.Get,
		}))
		e := echo.New()
		e.GET("/customers", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/customers?_format=text/csv", nil)
		req.Header.Set(echo.HeaderContentType, "text/csv")
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

		if resp.Header().Get("Content-Type") != "text/csv" {
			t.Errorf("expected content type to be %s got %s", "text/csv", resp.Header().Get("Content-Type"))
		}

		if !strings.Contains(resp.Header().Get("Content-Disposition"), repository.Name()) {
			t.Errorf("expected Content-Disposition header to contain  %s got %s", repository.Name(), resp.Header().Get("Content-Disposition"))
		}

		results := resp.Body.String()

		if !strings.Contains(results, "id") {
			t.Errorf("expected id to be in csv file")
		}

	})

	t.Run("test _format Omit sequenceNo, tablealias and weosid", func(t *testing.T) {
		container := &ContainerMock{
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
			GetWeOSConfigFunc: func() *rest.APIConfig {
				return nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Customer"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return nil, nil
			},
			CreateEntityWithValuesFunc: func(ctx context3.Context, payload []byte) (*model.ContentEntity, error) {
				return new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Customer"].Value, []byte(`{}`))
			},
			SchemaFunc: func() *openapi3.Schema {
				return swagger.Components.Schemas["Customer"].Value
			},
			GetListFunc: func(ctx context3.Context, entityFactory model.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]*model.ContentEntity, int64, error) {
				entity1, _ := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Customer"].Value, []byte(`{ "weos_id":"001","id":123,"firstName":"John", "lastName":"Doe", "sequence_no":"0", "table_alias":""}`))
				entity2, _ := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Customer"].Value, []byte(`{ "weos_id":"002","id":123,"firstName":"Jane", "lastName":"Doe", "sequence_no":"0", "table_alias":""}`))
				var entities []*model.ContentEntity
				entities = append(entities, entity1)
				entities = append(entities, entity2)
				return entities, 2, nil
			},
		}

		path := swagger.Paths.Find("/customers")

		mw := rest.Context(container, &CommandDispatcherMock{}, repository, path, path.Get)

		controller := mw(rest.DefaultListController(container, &CommandDispatcherMock{}, repository, map[string]*openapi3.PathItem{
			"/customers": path,
		}, map[string]*openapi3.Operation{
			http.MethodGet: path.Get,
		}))
		e := echo.New()
		e.GET("/customers", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/customers?_format=text/csv", nil)
		req.Header.Set(echo.HeaderContentType, "text/csv")
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

		if resp.Header().Get("Content-Type") != "text/csv" {
			t.Errorf("expected content type to be %s got %s", "text/csv", resp.Header().Get("Content-Type"))
		}

		if !strings.Contains(resp.Header().Get("Content-Disposition"), repository.Name()) {
			t.Errorf("expected Content-Disposition header to contain  %s got %s", repository.Name(), resp.Header().Get("Content-Disposition"))
		}

		results := resp.Body.String()

		if !strings.Contains(results, "id") {
			t.Errorf("expected id to be in csv file")
		}

		if strings.Contains(results, "weos_id") {
			t.Errorf("csv file must not contain weos_id")
		}

		if strings.Contains(results, "sequence_no") {
			t.Errorf("csv file must not contain sequence_no")
		}

		if strings.Contains(results, "table_alias") {
			t.Errorf("csv file must not contain table_alias")
		}
	})
	t.Run("test empty database", func(t *testing.T) {
		container := &ContainerMock{
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
			GetWeOSConfigFunc: func() *rest.APIConfig {
				return nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Customer"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return nil, nil
			},
			CreateEntityWithValuesFunc: func(ctx context3.Context, payload []byte) (*model.ContentEntity, error) {
				return new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Customer"].Value, []byte(`{}`))
			},
			SchemaFunc: func() *openapi3.Schema {
				return swagger.Components.Schemas["Customer"].Value
			},
			GetListFunc: func(ctx context3.Context, entityFactory model.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]*model.ContentEntity, int64, error) {
				var entities []*model.ContentEntity
				return entities, 0, nil
			},
		}

		path := swagger.Paths.Find("/customers")

		mw := rest.Context(container, &CommandDispatcherMock{}, repository, path, path.Get)

		controller := mw(rest.DefaultListController(container, &CommandDispatcherMock{}, repository, map[string]*openapi3.PathItem{
			"/customers": path,
		}, map[string]*openapi3.Operation{
			http.MethodGet: path.Get,
		}))
		e := echo.New()
		e.GET("/customers", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/customers?_format=text/csv", nil)
		req.Header.Set(echo.HeaderContentType, "text/csv")
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

		if resp.Header().Get("Content-Type") != "text/csv" {
			t.Errorf("expected content type to be %s got %s", "text/csv", resp.Header().Get("Content-Type"))
		}

		if !strings.Contains(resp.Header().Get("Content-Disposition"), repository.Name()) {
			t.Errorf("expected Content-Disposition header to contain  %s got %s", repository.Name(), resp.Header().Get("Content-Disposition"))
		}

		results := resp.Body.String()

		if !strings.Contains(results, "id") {
			t.Errorf("expected id to be in csv file")
		}

		if !strings.Contains(results, "firstName") {
			t.Errorf("expected firstName to be in csv file")
		}

		if !strings.Contains(results, "lastName") {
			t.Errorf("expected lastName to be in csv file")
		}
	})

	t.Run("test _sorts filter", func(t *testing.T) {
		swagger, err = LoadConfig(t, "./fixtures/blog.yaml")
		if err != nil {
			t.Fatalf("error loading api config '%s'", err)
		}

		container := &ContainerMock{
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
			GetWeOSConfigFunc: func() *rest.APIConfig {
				return nil
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
				if sortOptions == nil {
					t.Errorf("sort options not found")
				}

				if title, ok := sortOptions["title"]; ok {
					if title != "asc" {
						t.Errorf("expected title sort option to be asc got %s", title)
					}
				} else {
					t.Errorf("expected the sort option to be title")
				}

				entity, _ := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Blog"].Value, []byte(`{"id":1,"title":"test"}`))
				return []*model.ContentEntity{entity}, 1, nil
			},
		}

		path := swagger.Paths.Find("/blogs")

		mw := rest.Context(container, &CommandDispatcherMock{}, repository, path, path.Get)

		controller := mw(rest.DefaultListController(container, &CommandDispatcherMock{}, repository, map[string]*openapi3.PathItem{
			"/blogs": path,
		}, map[string]*openapi3.Operation{
			http.MethodGet: path.Get,
		}))
		e := echo.New()
		e.GET("/blogs", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/blogs", nil)
		req.Header.Set(echo.HeaderContentType, "application/json")
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("expected status code %d, got %d", http.StatusOK, resp.Code)
		}

		if len(repository.GetListCalls()) != 1 {
			t.Errorf("expected repository.GetList to be called once, got %d", len(repository.GetListCalls()))
		}
	})

	t.Run("test no _sorts filter", func(t *testing.T) {
		swagger, err = LoadConfig(t, "./fixtures/blog.yaml")
		if err != nil {
			t.Fatalf("error loading api config '%s'", err)
		}

		container := &ContainerMock{
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{}, nil
			},
			GetWeOSConfigFunc: func() *rest.APIConfig {
				return nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Post"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return nil, nil
			},
			CreateEntityWithValuesFunc: func(ctx context3.Context, payload []byte) (*model.ContentEntity, error) {
				return new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Post"].Value, []byte(`{}`))
			},
			SchemaFunc: func() *openapi3.Schema {
				return swagger.Components.Schemas["Post"].Value
			},
			GetListFunc: func(ctx context3.Context, entityFactory model.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]*model.ContentEntity, int64, error) {
				if sortOptions == nil {
					t.Errorf("sort options not found")
				}

				if id, ok := sortOptions["id"]; ok {
					if id != "asc" {
						t.Errorf("expected title sort option to be asc got %s", id)
					}
				} else {
					t.Errorf("expected the sort option to be id")
				}

				entity, _ := new(model.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas["Post"].Value, []byte(`{"id":1,"title":"first post"}`))
				return []*model.ContentEntity{entity}, 1, nil
			},
		}

		path := swagger.Paths.Find("/posts/")

		mw := rest.Context(container, &CommandDispatcherMock{}, repository, path, path.Get)

		controller := mw(rest.DefaultListController(container, &CommandDispatcherMock{}, repository, map[string]*openapi3.PathItem{
			"/posts/": path,
		}, map[string]*openapi3.Operation{
			http.MethodGet: path.Get,
		}))
		e := echo.New()
		e.GET("/posts/", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/posts/", nil)
		req.Header.Set(echo.HeaderContentType, "application/json")
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("expected status code %d, got %d", http.StatusOK, resp.Code)
		}

		if len(repository.GetListCalls()) != 1 {
			t.Errorf("expected repository.GetList to be called once, got %d", len(repository.GetListCalls()))
		}
	})
}
