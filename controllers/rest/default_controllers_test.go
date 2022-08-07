package rest_test

import (
	context3 "context"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"golang.org/x/net/context"
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
		}
		commandDispatcher := &CommandDispatcherMock{
			DispatchFunc: func(ctx context.Context, command *model.Command, container model.Container, eventStore model.EventRepository, projection model.Projection, logger model.Log) error {
				if command.Type != model.CREATE_COMMAND {
					t.Errorf("expected command type '%s', got '%s'", model.CREATE_COMMAND, command.Type)
				}
				return nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
		}

		path := swagger.Paths.Find("/blogs")

		controller := rest.DefaultWriteController(container, commandDispatcher, repository, path.Post)
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
		}
		commandDispatcher := &CommandDispatcherMock{
			DispatchFunc: func(ctx context.Context, command *model.Command, container model.Container, eventStore model.EventRepository, projection model.Projection, logger model.Log) error {
				if command.Type != model.UPDATE_COMMAND {
					t.Errorf("expected command type '%s', got '%s'", model.UPDATE_COMMAND, command.Type)
				}
				return nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
		}

		path := swagger.Paths.Find("/blogs/:id")

		controller := rest.DefaultWriteController(container, commandDispatcher, repository, path.Put)
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
		}
		commandDispatcher := &CommandDispatcherMock{
			DispatchFunc: func(ctx context.Context, command *model.Command, container model.Container, eventStore model.EventRepository, projection model.Projection, logger model.Log) error {
				if command.Type != model.DELETE_COMMAND {
					t.Errorf("expected command type '%s', got '%s'", model.DELETE_COMMAND, command.Type)
				}
				return nil
			},
		}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
		}

		path := swagger.Paths.Find("/blogs/:id")

		controller := rest.DefaultWriteController(container, commandDispatcher, repository, path.Delete)
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
		container := &ContainerMock{}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return nil, nil
			},
		}

		path := swagger.Paths.Find("/blogs/:id")

		controller := rest.DefaultReadController(container, &CommandDispatcherMock{}, repository, path.Get)
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

		//TODO check that the response body is correct
	})

	t.Run("test get list of items", func(t *testing.T) {
		container := &ContainerMock{}
		repository := &EntityRepositoryMock{
			NameFunc: func() string {
				return "Blog"
			},
			GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
				return nil, nil
			},
		}

		path := swagger.Paths.Find("/blogs")

		controller := rest.DefaultReadController(container, &CommandDispatcherMock{}, repository, path.Get)
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
			t.Errorf("expected repository.GetByKey to be called once, got %d", len(repository.GetByKeyCalls()))
		}

		if len(resp.Body.String()) == 0 {
			t.Errorf("expected body to be not empty")
		}

		//TODO check that the response body is correct
	})
}
