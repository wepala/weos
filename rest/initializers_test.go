package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/v2/rest"
	"golang.org/x/net/context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouteInitializer(t *testing.T) {
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
	t.Run("Default routes", func(t *testing.T) {
		commandDispatcher := &CommandDispatcherMock{}
		repository := &rest.ResourceRepository{}
		e := echo.New()
		params := rest.RouteParams{
			CommandDispatcher:  commandDispatcher,
			ResourceRepository: repository,
			Logger:             logger,
			Echo:               e,
			Config:             schema,
			APIConfig:          &rest.APIConfig{},
		}
		err = rest.RouteInitializer(params)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		hasHealthEndpoint := false
		for _, route := range e.Routes() {
			if route.Path == "/health" {
				hasHealthEndpoint = true
			}
		}
		if !hasHealthEndpoint {
			t.Fatalf("expected to find /health endpoint")
		}
	})

	t.Run("custom controllers", func(t *testing.T) {
		commandDispatcher := &CommandDispatcherMock{}
		eventStore := &EventStoreMock{
			PersistFunc: func(ctxt context.Context, logger rest.Log, resources []rest.Resource) []error {
				return nil
			},
			AddSubscriberFunc: func(handler rest.EventHandlerConfig) error {
				return nil
			},
			GetEventHandlersFunc: func() []rest.EventHandlerConfig {
				return nil
			},
			GetByURIFunc: func(ctxt context.Context, logger rest.Log, uri string) (rest.Resource, error) {
				return &rest.BasicResource{
					Metadata: rest.ResourceMetadata{ID: "/accounts/123/insights/3a"},
				}, nil
			},
		}
		repository, err := rest.NewResourceRepository(rest.ResourceRepositoryParams{
			EventStore:        eventStore,
			DefaultProjection: eventStore,
		})
		e := echo.New()
		params := rest.RouteParams{
			CommandDispatcher:  commandDispatcher,
			ResourceRepository: repository.Repository,
			Logger:             logger,
			Echo:               e,
			Config:             schema,
			APIConfig: &rest.APIConfig{
				ServiceConfig: &rest.ServiceConfig{},
			},
			Controllers: []map[string]rest.Controller{
				{
					"helloWorld": func(params *rest.ControllerParams) echo.HandlerFunc {
						return func(ctxt echo.Context) error {
							return ctxt.String(200, "Hello World")
						}
					},
				},
			},
		}
		err = rest.RouteInitializer(params)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		endpoint := false
		for _, route := range e.Routes() {
			if route.Path == "/another" {
				endpoint = true
			}
		}
		if !endpoint {
			t.Fatalf("expected to find /another endpoint")
		}

		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/another", nil)
		req.Header.Set(echo.HeaderContentType, "application/json")
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("expected status code %d, got %d", http.StatusOK, resp.Code)
		}
		if resp.Body.String() != "Hello World" {
			t.Errorf("expected response body to be 'Hello World', got %s", resp.Body.String())
		}

		//test default route to see that it still works
		resp = httptest.NewRecorder()
		payload := `{
    "@context": {
        "weos":"https://schema.weos.cloud/v1"
    },
    "@id": "/accounts/123/insights/3",
    "@type": "weos:Insight",
    "accountId": "/accounts/123",
    "active": false,
    "created": "2024/04/04",
    "insight": "Your credit card utilization rate is currently at 35%, which could impact your credit score negatively",
    "updated": "2024/04/04",
    "userId": "/user/123"
}`
		req = httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(payload))
		req.Header.Set(echo.HeaderContentType, "application/ld+json")
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusCreated {
			t.Errorf("expected status code %d, got %d", http.StatusCreated, resp.Code)
		}
	})

	t.Run("each route should have an options path that returns the available headers methods etc", func(t *testing.T) {
		commandDispatcher := &CommandDispatcherMock{}
		eventStore := &EventStoreMock{
			PersistFunc: func(ctxt context.Context, logger rest.Log, resources []rest.Resource) []error {
				return nil
			},
			AddSubscriberFunc: func(handler rest.EventHandlerConfig) error {
				return nil
			},
			GetEventHandlersFunc: func() []rest.EventHandlerConfig {
				return nil
			},
			GetByURIFunc: func(ctxt context.Context, logger rest.Log, uri string) (rest.Resource, error) {
				return &rest.BasicResource{
					Metadata: rest.ResourceMetadata{ID: "/accounts/123/insights/3a"},
				}, nil
			},
		}
		repository, err := rest.NewResourceRepository(rest.ResourceRepositoryParams{
			EventStore:        eventStore,
			DefaultProjection: eventStore,
		})
		e := echo.New()
		params := rest.RouteParams{
			CommandDispatcher:  commandDispatcher,
			ResourceRepository: repository.Repository,
			Logger:             logger,
			Echo:               e,
			Config:             schema,
			APIConfig: &rest.APIConfig{
				ServiceConfig: &rest.ServiceConfig{},
			},
			Controllers: []map[string]rest.Controller{
				{
					"helloWorld": func(params *rest.ControllerParams) echo.HandlerFunc {
						return func(ctxt echo.Context) error {
							return ctxt.String(200, "Hello World")
						}
					},
				},
			},
		}
		err = rest.RouteInitializer(params)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		endpoint := false
		for _, route := range e.Routes() {
			if route.Path == "/another" {
				endpoint = true
			}
		}
		if !endpoint {
			t.Fatalf("expected to find /another endpoint")
		}

		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/another", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		req.Header.Set(echo.HeaderContentType, "application/json")
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusNoContent {
			t.Errorf("expected status code %d, got %d", http.StatusNoContent, resp.Code)
		}
		headers := resp.Header()
		if headers.Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("expected Access-Control-Allow-Origin header to be set, got '%s'", headers.Get("Access-Control-Allow-Origin"))
		}
		if headers.Get("Access-Control-Allow-Methods") != "GET,OPTIONS,HEAD" {
			t.Errorf("expected Access-Control-Allow-Methods header to be set, got '%s'", headers.Get("Access-Control-Allow-Methods"))
		}
	})

}
