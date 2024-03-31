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
	t.Run("create a simple resource for the first time", func(t *testing.T) {
		defaultProjection := &ProjectionMock{
			GetByURIFunc: func(ctxt context.Context, logger rest.Log, uri string) (rest.Resource, error) {
				return nil, nil
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
		commandDispatcher := &CommandDispatcherMock{
			DispatchFunc: func(ctx context.Context, command *rest.Command, repository *rest.ResourceRepository, logger rest.Log) (interface{}, error) {
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
	})
}
