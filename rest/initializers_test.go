package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/v2/rest"
	"net/http"
	"net/http/httptest"
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
		repository := &rest.ResourceRepository{}
		e := echo.New()
		params := rest.RouteParams{
			CommandDispatcher:  commandDispatcher,
			ResourceRepository: repository,
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
	})
}
