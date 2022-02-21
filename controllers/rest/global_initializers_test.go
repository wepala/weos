package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"golang.org/x/net/context"
	"testing"
)

func TestGlobalMiddlewareInitializer(t *testing.T) {
	api, err := rest.New("./fixtures/blog-security.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	baseCtxt := context.WithValue(context.TODO(), weoscontext.SCHEMA_BUILDERS, schemas)

	middlewareCalled := false
	api.RegisterMiddleware("AuthorizationMiddleware", func(api *rest.RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
		return func(handlerFunc echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				middlewareCalled = true
				return nil
			}
		}
	})
	t.Run("auth middleware was added to context", func(t *testing.T) {
		ctxt, err := rest.Security(baseCtxt, api, api.Swagger)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		middlewares := rest.GetOperationMiddlewares(ctxt)
		if len(middlewares) != 1 {
			t.Fatalf("expected the middlewares in context to be %d, got %d", 1, len(middlewares))
		}
		for _, middleware := range middlewares {
			err = middleware(api, nil, nil, nil, nil, nil, nil)(func(c echo.Context) error {
				return nil
			})(echo.New().AcquireContext())
			if err != nil {
				t.Errorf("unexpected error running middleware '%s'", err)
			}
		}
		if !middlewareCalled {
			t.Errorf("expected middleware to be in context and called")
		}
	})
}
