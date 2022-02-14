package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"golang.org/x/net/context"
	"net/http"
	"testing"
)

func TestEntityFactoryInitializer(t *testing.T) {
	api, err := rest.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	baseCtxt := context.WithValue(context.TODO(), weoscontext.SCHEMA_BUILDERS, schemas)
	t.Run("get schema from request body", func(t *testing.T) {

		ctxt, err := rest.EntityFactoryInitializer(baseCtxt, api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		entityFactory := rest.GetEntityFactory(ctxt)
		if entityFactory == nil {
			t.Fatalf("expected entity factory to be in the context")
		}
		if entityFactory.Name() != "Blog" {
			t.Errorf("expected the factory name to be '%s', got '%s'", "Blog", entityFactory.Name())
		}
	})
	t.Run("get schema from items in request body", func(t *testing.T) {
		api, err = rest.New("./fixtures/blog-create-batch.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		ctxt, err := rest.EntityFactoryInitializer(baseCtxt, api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		entityFactory := rest.GetEntityFactory(ctxt)
		if entityFactory == nil {
			t.Fatalf("expected entity factory to be in the context")
		}
		if entityFactory.Name() != "Blog" {
			t.Errorf("expected the factory name to be '%s', got '%s'", "Blog", entityFactory.Name())
		}
	})
	t.Run("use the x-schema extension to specify schema", func(t *testing.T) {
		api, err = rest.New("./fixtures/blog-pk-guid-title.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		ctxt, err := rest.EntityFactoryInitializer(baseCtxt, api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		entityFactory := rest.GetEntityFactory(ctxt)
		if entityFactory == nil {
			t.Fatalf("expected entity factory to be in the context")
		}
		if entityFactory.Name() != "Blog" {
			t.Errorf("expected the factory name to be '%s', got '%s'", "Blog", entityFactory.Name())
		}
	})

}

func TestUserDefinedInitializer(t *testing.T) {
	apiConfig := `
openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
x-weos-config:
  event-source:
    - title: default
      driver: service
      endpoint: https://prod1.weos.sh/events/v1
    - title: event
      driver: sqlite3
      database: test.db
  database:
    driver: sqlite3
    database: test.db
  databases:
    - title: default
      driver: sqlite3
      database: test.db
  rest:
    middleware:
      - RequestID
      - Recover
      - ZapLogger
components:
  schemas:
    Post:
      type: object
      properties:
        title:
          type: string
        description:
          type: string
        created:
          type: string
          format: date-time
paths:
  /health:
    summary: Health Check
    get:
      x-controller: HealthCheck
      x-middleware:
        - Recover
      x-command-dispatcher: HealthCheck
      x-event-store: HealthCheck
      x-projection: HealthCheck	
      responses:
        200:
          description: Health Response
        500:
          description: API Internal Error
`
	api, err := rest.New(apiConfig)
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	baseCtxt := context.WithValue(context.TODO(), weoscontext.SCHEMA_BUILDERS, schemas)

	api.RegisterController("HealthCheck", rest.HealthCheck)

	middlewareCalled := false
	api.RegisterMiddleware("Recover", func(api *rest.RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
		return func(handlerFunc echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				middlewareCalled = true
				return nil
			}
		}
	})

	api.RegisterCommandDispatcher("HealthCheck", &CommandDispatcherMock{
		DispatchFunc: func(ctx context.Context, command *model.Command, eventStore model.EventRepository, projection model.Projection, logger model.Log) error {
			return nil
		}})
	api.RegisterEventStore("HealthCheck", &EventRepositoryMock{})
	api.RegisterProjection("HealthCheck", &ProjectionMock{})

	t.Run("attach user defined controller", func(t *testing.T) {
		ctxt, err := rest.UserDefinedInitializer(baseCtxt, api, "/health", http.MethodGet, api.Swagger, api.Swagger.Paths["/health"], api.Swagger.Paths["/health"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})

	t.Run("attach user defined middleware", func(t *testing.T) {
		ctxt, err := rest.UserDefinedInitializer(baseCtxt, api, "/health", http.MethodGet, api.Swagger, api.Swagger.Paths["/health"], api.Swagger.Paths["/health"].Get)
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

	t.Run("add user defined command dispatcher", func(t *testing.T) {
		ctxt, err := rest.UserDefinedInitializer(baseCtxt, api, "/health", http.MethodGet, api.Swagger, api.Swagger.Paths["/health"], api.Swagger.Paths["/health"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		commandDispatcher := rest.GetOperationCommandDispatcher(ctxt)
		if commandDispatcher == nil {
			t.Fatalf("expected the command dispatcher to be in context but got nil")
		}
	})

	t.Run("add user defined event repository", func(t *testing.T) {
		ctxt, err := rest.UserDefinedInitializer(baseCtxt, api, "/health", http.MethodGet, api.Swagger, api.Swagger.Paths["/health"], api.Swagger.Paths["/health"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		eventStore := rest.GetOperationEventStore(ctxt)
		if eventStore == nil {
			t.Fatalf("expected the event store to be in context but got nil")
		}
	})

	t.Run("add user defined projection", func(t *testing.T) {
		ctxt, err := rest.UserDefinedInitializer(baseCtxt, api, "/health", http.MethodGet, api.Swagger, api.Swagger.Paths["/health"], api.Swagger.Paths["/health"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		projection := rest.GetOperationProjection(ctxt)
		if projection == nil {
			t.Fatalf("expected the projection to be in context but got nil")
		}
	})
}

func TestStandardInitializer(t *testing.T) {
	api, err := rest.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	baseCtxt := context.WithValue(context.TODO(), weoscontext.SCHEMA_BUILDERS, schemas)
	api.RegisterController("CreateController", rest.CreateController)
	api.RegisterController("ListController", rest.ListController)
	api.RegisterController("UpdateController", rest.UpdateController)
	api.RegisterController("ViewController", rest.ViewController)
	api.RegisterController("DeleteController", rest.DeleteController)
	t.Run("attach standard create", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(baseCtxt, api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})

	t.Run("attach standard list view ", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(baseCtxt, api, "/posts/", http.MethodGet, api.Swagger, api.Swagger.Paths["/posts/"], api.Swagger.Paths["/posts/"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})

	t.Run("attach standard list view with alias ", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(baseCtxt, api, "/blogs", http.MethodGet, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})
	t.Run("attach standard view", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(baseCtxt, api, "/blogs/{id}", http.MethodGet, api.Swagger, api.Swagger.Paths["/blogs/{id}"], api.Swagger.Paths["/blogs/{id}"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})
	t.Run("attach standard update", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(baseCtxt, api, "/blogs/{}", http.MethodPut, api.Swagger, api.Swagger.Paths["/blogs/{id}"], api.Swagger.Paths["/blogs/{id}"].Put)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})
	t.Run("attach standard delete", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(baseCtxt, api, "/blogs/{id}", http.MethodDelete, api.Swagger, api.Swagger.Paths["/blogs/{id}"], api.Swagger.Paths["/blogs/{id}"].Delete)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})
}
