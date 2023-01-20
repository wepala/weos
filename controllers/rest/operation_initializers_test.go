package rest_test

import (
	context3 "context"
	"fmt"
	"github.com/casbin/casbin/v2"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEntityRepositoryInitializer(t *testing.T) {
	api, err := rest.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	baseCtxt, err := rest.SQLDatabase(context.TODO(), api, api.Swagger)
	api.RegisterLog("Default", &LogMock{})
	t.Run("get schema from request body", func(t *testing.T) {
		baseCtxt, err = rest.RegisterEntityRepositories(baseCtxt, api, api.Swagger)
		if err != nil {
			t.Fatalf("error setting up entity repositories %s", err)
		}
		ctxt, err := rest.EntityRepositoryInitializer(baseCtxt, api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		entityFactory := rest.GetEntityRepository(ctxt)
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
		api.RegisterLog("Default", &LogMock{})
		baseCtxt, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		baseCtxt, err = rest.DefaultProjection(baseCtxt, api, api.Swagger)
		baseCtxt, err = rest.RegisterEntityRepositories(baseCtxt, api, api.Swagger)
		if err != nil {
			t.Fatalf("error setting up entity repositories %s", err)
		}
		api.RegisterLog("Default", &LogMock{})
		ctxt, err := rest.EntityRepositoryInitializer(baseCtxt, api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		entityFactory := rest.GetEntityRepository(ctxt)
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
		api.RegisterLog("Default", &LogMock{})
		baseCtxt, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		baseCtxt, err = rest.DefaultProjection(baseCtxt, api, api.Swagger)
		baseCtxt, err = rest.RegisterEntityRepositories(baseCtxt, api, api.Swagger)
		if err != nil {
			t.Fatalf("error setting up entity repositories %s", err)
		}
		api.RegisterLog("Default", &LogMock{})
		ctxt, err := rest.EntityRepositoryInitializer(baseCtxt, api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		entityFactory := rest.GetEntityRepository(ctxt)
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
		api.RegisterLog("Default", &LogMock{})
		baseCtxt, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		baseCtxt, err = rest.DefaultProjection(baseCtxt, api, api.Swagger)
		baseCtxt, err = rest.RegisterEntityRepositories(baseCtxt, api, api.Swagger)
		if err != nil {
			t.Fatalf("error setting up entity repositories %s", err)
		}
		api.RegisterLog("Default", &LogMock{})
		ctxt, err := rest.EntityRepositoryInitializer(baseCtxt, api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		entityFactory := rest.GetEntityRepository(ctxt)
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
	api.RegisterMiddleware("Recover", func(api rest.Container, commandDispatcher model.CommandDispatcher, eventSource model.EntityRepository, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
		return func(handlerFunc echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				middlewareCalled = true
				return nil
			}
		}
	})

	api.RegisterCommandDispatcher("HealthCheck", &CommandDispatcherMock{
		DispatchFunc: func(ctx context3.Context, command *model.Command, container model.Container, repository model.EntityRepository, logger model.Log) (interface{}, error) {
			return nil, nil
		}})
	api.RegisterEventStore("HealthCheck", &EventRepositoryMock{})
	api.RegisterProjection("Default", &ProjectionMock{})
	api.RegisterProjection("Custom", &ProjectionMock{})
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
			err = middleware(api, nil, nil, api.Swagger.Paths["/health"], api.Swagger.Paths["/health"].Get)(func(c echo.Context) error {
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
	api.RegisterController("DefaultWriteController", rest.DefaultWriteController)
	api.RegisterController("DefaultReadController", rest.DefaultReadController)
	api.RegisterController("DefaultListController", rest.DefaultReadController)

	//api.RegisterController("ListController", rest.ListController)
	t.Run("attach standard create", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(context.TODO(), api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})

	t.Run("attach standard list view ", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(context.TODO(), api, "/posts/", http.MethodGet, api.Swagger, api.Swagger.Paths["/posts/"], api.Swagger.Paths["/posts/"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})

	t.Run("attach standard list view with alias ", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(context.TODO(), api, "/blogs", http.MethodGet, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})
	t.Run("attach standard view", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(context.TODO(), api, "/blogs/{id}", http.MethodGet, api.Swagger, api.Swagger.Paths["/blogs/{id}"], api.Swagger.Paths["/blogs/{id}"].Get)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})
	t.Run("attach standard update", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(context.TODO(), api, "/blogs/{}", http.MethodPut, api.Swagger, api.Swagger.Paths["/blogs/{id}"], api.Swagger.Paths["/blogs/{id}"].Put)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		controller := rest.GetOperationController(ctxt)
		if controller == nil {
			t.Fatalf("expected controller to be in the context")
		}
	})
	t.Run("attach standard delete", func(t *testing.T) {
		ctxt, err := rest.StandardInitializer(context.TODO(), api, "/blogs/{id}", http.MethodDelete, api.Swagger, api.Swagger.Paths["/blogs/{id}"], api.Swagger.Paths["/blogs/{id}"].Delete)
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
	api.RegisterController("DefaultWriteController", rest.DefaultWriteController)
	api.RegisterController("DefaultReadController", rest.DefaultReadController)
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
	api.RegisterLog("Default", logger)

	api.RegisterController("DefaultReadController", func(api rest.Container, commandDispatcher model.CommandDispatcher, repository model.EntityRepository, path map[string]*openapi3.PathItem, operation map[string]*openapi3.Operation) echo.HandlerFunc {
		return func(c echo.Context) error {
			controllerTriggered = true
			return nil
		}
	})
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

func TestAuthorizationInitializer(t *testing.T) {
	api, err := rest.New("./fixtures/blog-security.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	err = api.Initialize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error initializing api '%s'", err)
	}
	swagger := api.Swagger

	t.Run("setup default enforcer", func(t *testing.T) {
		container := &ContainerMock{
			GetPermissionEnforcerFunc: func(name string) (*casbin.Enforcer, error) {
				return nil, fmt.Errorf("enforcer named '%s' not found", name)
			},
			RegisterPermissionEnforcerFunc: func(name string, enforcer *casbin.Enforcer) {

			},
			GetGormDBConnectionFunc: func(name string) (*gorm.DB, error) {
				return api.GetGormDBConnection(name)
			},
		}
		path := swagger.Paths.Find("/blogs")
		_, err := rest.AuthorizationInitializer(context.TODO(), container, "/blogs", "POST", swagger, path, path.Post)
		if err != nil {
			t.Fatalf("unexpected error setting up authorization '%s'", err)
		}
		if len(container.RegisterPermissionEnforcerCalls()) != 1 {
			t.Fatalf("expected default enforcer to be set")
		}
	})
}
