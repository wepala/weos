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
	t.Run("create a simple resource", func(t *testing.T) {
		repository := &RepositoryMock{
			PersistFunc: func(ctxt context.Context, logger rest.Log, resources []rest.Resource) []error {
				return nil
			},
		}
		commandDispatcher := &CommandDispatcherMock{
			DispatchFunc: func(ctx context.Context, command *rest.Command, repository rest.Repository, logger rest.Log) (interface{}, error) {
				return nil, nil
			},
		}
		controller := rest.DefaultWriteController(logger, commandDispatcher, repository, schema, nil, nil)
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

		if len(commandDispatcher.DispatchCalls()) != 1 {
			t.Errorf("expected dispatch to be called once, got %d", len(commandDispatcher.DispatchCalls()))
		}

	})
}
