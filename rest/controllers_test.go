package rest_test

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/v2/rest"
	"golang.org/x/net/context"
	"gorm.io/datatypes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultWriteController(t *testing.T) {
	schema, err := openapi3.NewLoader().LoadFromFile("fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error encountered loading schema '%s'", err)
	}
	logger := &LogMock{
		DebugfFunc: func(format string, args ...interface{}) {

		},
		DebugFunc: func(args ...interface{}) {

		},
		ErrorfFunc: func(format string, args ...interface{}) {

		},
		ErrorFunc: func(args ...interface{}) {

		},
	}
	t.Run("create a simple resource for the first time", func(t *testing.T) {
		defaultProjection := &ProjectionMock{
			GetByURIFunc: func(ctxt context.Context, logger rest.Log, uri string) (rest.Resource, error) {
				return nil, nil
			},
			GetEventHandlersFunc: func() []rest.EventHandlerConfig {
				return nil
			},
		}
		eventStore := &EventStoreMock{
			AddSubscriberFunc: func(config rest.EventHandlerConfig) error {
				return nil
			},
			PersistFunc: func(ctxt context.Context, logger rest.Log, resources []rest.Resource) []error {
				return nil
			},
		}
		params := rest.ResourceRepositoryParams{
			EventStore:        eventStore,
			DefaultProjection: defaultProjection,
			Config:            schema,
		}
		result, err := rest.NewResourceRepository(params)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		repository := result.Repository
		commandDispatcher := &CommandDispatcherMock{
			DispatchFunc: func(ctx context.Context, logger rest.Log, command *rest.Command, options *rest.CommandOptions) (rest.CommandResponse, error) {
				return rest.CommandResponse{
					Code: 200,
				}, nil
			},
		}
		controller := rest.DefaultWriteController(&rest.ControllerParams{
			Logger:             logger,
			CommandDispatcher:  commandDispatcher,
			ResourceRepository: repository,
			Schema:             schema,
			PathMap:            nil,
			Operation:          nil,
		})
		e := echo.New()
		e.POST("/*", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.POST, "/blogs/test", strings.NewReader(`{
        "@id": "/blogs/test",
		"@type": "http://schema.org/Blog",
		"title":"test"
}`))
		req.Header.Set(echo.HeaderContentType, "application/ld+json")
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusCreated {
			t.Errorf("expected status code %d, got %d", http.StatusCreated, resp.Code)
		}
	})
}

func TestDefaultReadController(t *testing.T) {
	schema, err := openapi3.NewLoader().LoadFromFile("fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error encountered loading schema '%s'", err)
	}
	logger := &LogMock{
		DebugfFunc: func(format string, args ...interface{}) {

		},
		DebugFunc: func(args ...interface{}) {

		},
		ErrorfFunc: func(format string, args ...interface{}) {

		},
		ErrorFunc: func(args ...interface{}) {

		},
	}
	t.Run("get a simple resource", func(t *testing.T) {
		commandDispatcher := &CommandDispatcherMock{}
		defaultProjection := &ProjectionMock{
			GetByURIFunc: func(ctxt context.Context, logger rest.Log, uri string) (rest.Resource, error) {
				resource := &rest.BasicResource{
					Body: make(datatypes.JSONMap),
					Metadata: rest.ResourceMetadata{
						ID:         "/blogs/test",
						Type:       "http://schema.org/Blog",
						SequenceNo: 1,
					},
				}
				resource.Body["title"] = "test"
				return resource, nil
			},
			GetEventHandlersFunc: func() []rest.EventHandlerConfig {
				return nil
			},
		}
		eventStore := &EventStoreMock{
			AddSubscriberFunc: func(config rest.EventHandlerConfig) error {
				return nil
			},
		}
		params := rest.ResourceRepositoryParams{
			EventStore:        eventStore,
			DefaultProjection: defaultProjection,
			Config:            schema,
		}
		result, err := rest.NewResourceRepository(params)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		repository := result.Repository
		controller := rest.DefaultReadController(&rest.ControllerParams{
			Logger:             logger,
			CommandDispatcher:  commandDispatcher,
			ResourceRepository: repository,
			Schema:             schema,
			PathMap:            nil,
			Operation:          nil,
		})

		e := echo.New()
		e.GET("/blog/test", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/blog/test", nil)
		req.Header.Set(echo.HeaderContentType, "application/ld+json")
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("expected status code %d, got %d", http.StatusOK, resp.Code)
		}
		var readResult map[string]interface{}
		err = json.Unmarshal(resp.Body.Bytes(), &readResult)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		if title, ok := readResult["title"]; !ok || title != "test" {
			t.Errorf("expected title to be 'test', got %s", title)
		}
	})
	t.Run("return 404 if the content is not ld+json and there isn't a matching json route", func(t *testing.T) {
		commandDispatcher := &CommandDispatcherMock{}
		defaultProjection := &ProjectionMock{
			GetByURIFunc: func(ctxt context.Context, logger rest.Log, uri string) (rest.Resource, error) {
				resource := &rest.BasicResource{
					Body: make(datatypes.JSONMap),
					Metadata: rest.ResourceMetadata{
						ID:         "/blogs/test",
						Type:       "http://schema.org/Blog",
						SequenceNo: 1,
					},
				}
				resource.Body["title"] = "test"
				return resource, nil
			},
			GetEventHandlersFunc: func() []rest.EventHandlerConfig {
				return nil
			},
		}
		eventStore := &EventStoreMock{
			AddSubscriberFunc: func(config rest.EventHandlerConfig) error {
				return nil
			},
		}
		params := rest.ResourceRepositoryParams{
			EventStore:        eventStore,
			DefaultProjection: defaultProjection,
			Config:            schema,
		}
		result, err := rest.NewResourceRepository(params)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		repository := result.Repository
		controller := rest.DefaultReadController(&rest.ControllerParams{
			Logger:             logger,
			CommandDispatcher:  commandDispatcher,
			ResourceRepository: repository,
			Schema:             schema,
			PathMap:            nil,
			Operation:          nil,
		})

		e := echo.New()
		e.GET("/blog/test", controller)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(echo.GET, "/blog/test", nil)
		req.Header.Set(echo.HeaderContentType, "application/json")
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusNotFound {
			t.Errorf("expected status code %d, got %d", http.StatusNotFound, resp.Code)
		}
	})
}
