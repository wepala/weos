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
	"net/http/httptest"
	"testing"
)

func TestEntityFactoryInitializer(t *testing.T) {
	api, err := rest.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	baseCtxt := context.WithValue(context.TODO(), weoscontext.SCHEMA_BUILDERS, schemas)
	api.Schemas = schemas
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
		api.Schemas = schemas
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
		api.Schemas = schemas
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
	t.Run("use the x-schema extension (with content path) to specify schema", func(t *testing.T) {
		api, err = rest.New("./fixtures/blog-x-schema.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		api.Schemas = schemas
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
      x-projections: 
        - Default
        - Custom
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
	api.RegisterMiddleware("Recover", func(api rest.Container, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
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
	api.RegisterProjection("Default", &ProjectionMock{})
	api.RegisterProjection("Custom", &ProjectionMock{})
	api.RegisterMiddleware("DefaultResponseMiddleware", rest.DefaultResponseMiddleware)
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
		if len(middlewares) != 2 {
			t.Fatalf("expected the middlewares in context to be %d, got %d", 2, len(middlewares))
		}
		ct := echo.New().AcquireContext()
		req := &http.Request{}
		ct.SetRequest(req)
		for _, middleware := range middlewares {
			err = middleware(api, nil, nil, nil, nil, api.Swagger.Paths["/health"], api.Swagger.Paths["/health"].Get)(func(c echo.Context) error {
				return nil
			})(ct)

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
		definedProjections := rest.GetOperationProjections(ctxt)
		if len(definedProjections) != 2 {
			t.Fatalf("expected %d projections, got %d projections", 2, len(definedProjections))
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

func TestRouteInitializer(t *testing.T) {
	controllerTriggered := false
	api, err := rest.New("./fixtures/blog-route-initializer.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	baseCtxt := context.WithValue(context.TODO(), weoscontext.SCHEMA_BUILDERS, schemas)
	api.RegisterController("CreateController", rest.CreateController)
	api.RegisterController("ListController", rest.ListController)
	api.RegisterController("UpdateController", rest.UpdateController)
	api.RegisterController("ViewController", func(api rest.Container, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {
		return func(c echo.Context) error {
			controllerTriggered = true
			if _, ok := projection.(*projections.MetaProjection); !ok {
				t.Fatalf("expected the projection to be a meta projection because there are multiple projections defined")
			}
			return nil
		}
	})
	api.RegisterController("DeleteController", rest.DeleteController)
	api.RegisterProjection("Custom", &ProjectionMock{
		MigrateFunc: func(ctx context.Context, schema *openapi3.Swagger) error {
			return nil
		},
		GetByKeyFunc: func(ctxt context.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
			return nil, nil
		},
	})
	api.RegisterProjection("Default", &ProjectionMock{
		MigrateFunc: func(ctx context.Context, schema *openapi3.Swagger) error {
			return nil
		},
		GetByKeyFunc: func(ctxt context.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
			return nil, nil
		},
	})
	api.RegisterCommandDispatcher("Default", &CommandDispatcherMock{})
	api.RegisterEventStore("Default", &EventRepositoryMock{})
	api.RegisterMiddleware("DefaultResponseMiddleware", rest.DefaultResponseMiddleware)
	t.Run("setup meta projection", func(t *testing.T) {
		ctxt, err := rest.UserDefinedInitializer(baseCtxt, api, "/blogs/{id}", http.MethodGet, api.Swagger, api.Swagger.Paths["/blogs/{id}"], api.Swagger.Paths["/blogs/{id}"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		ctxt, err = rest.StandardInitializer(ctxt, api, "/blogs/{id}", http.MethodGet, api.Swagger, api.Swagger.Paths["/blogs/{id}"], api.Swagger.Paths["/blogs/{id}"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		ctxt, err = rest.RouteInitializer(ctxt, api, "/blogs/{id}", http.MethodGet, api.Swagger, api.Swagger.Paths["/blogs/{id}"], api.Swagger.Paths["/blogs/{id}"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		e := api.EchoInstance()

		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs/1", nil)
		e.ServeHTTP(resp, req)
		if !controllerTriggered {
			t.Fatalf("expected the view controller to be tiggered")
		}
	})
}

func TestGettersForOperationFunctions(t *testing.T) {
	ctx := context.Background()

	t.Run("getting schema builder sending empty context", func(t *testing.T) {
		builders := rest.GetSchemaBuilders(ctx)
		if builders == nil {
			t.Errorf("unexpected error expected map of builders to be returned got nil")
		}
	})
}
