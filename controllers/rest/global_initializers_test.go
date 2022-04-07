package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"golang.org/x/net/context"
	"os"
	"testing"
)

func TestGlobalMiddlewareInitializer(t *testing.T) {
	api, err := rest.New("./fixtures/blog-security.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	baseCtxt := context.WithValue(context.TODO(), weoscontext.SCHEMA_BUILDERS, schemas)

	_, gormDB, err := api.SQLConnectionFromConfig(api.Config.Database)
	if err != nil {
		t.Fatalf("unexpected error opening db connection")
	}
	defaultProjection, err := projections.NewProjection(baseCtxt, gormDB, api.EchoInstance().Logger)
	if err != nil {
		t.Fatalf("unexpected error instantiating new projection")
	}
	api.RegisterProjection("Default", defaultProjection)

	middlewareCalled := false
	api.RegisterMiddleware("OpenIDMiddleware", func(api *rest.RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
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
	t.Run("session was added to the api", func(t *testing.T) {
		_, err := rest.Security(baseCtxt, api, api.Swagger)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		store := api.GetSessionStore()
		if store == nil {
			t.Fatalf("expected session store to be instantiated got nil")
		}
	})
	os.Remove("test.db")
}

func TestSQLDatabase(t *testing.T) {
	t.Run("legacy database config", func(t *testing.T) {
		apiYaml := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
  - url: http://localhost:8681
x-weos-config:
  database:
    driver: sqlite3
    database: test.db
`
		api, err := rest.New(apiYaml)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		if _, err = api.GetDBConnection("Default"); err != nil {
			t.Fatalf("expected a connection to be created")
		}

	})

	t.Run("no config no errors", func(t *testing.T) {
		apiYaml := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
  - url: http://localhost:8681
`
		api, err := rest.New(apiYaml)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		if err != nil {
			t.Errorf("unexpected error instantiating ")
		}
	})

	t.Run("multiple sql database connections", func(t *testing.T) {
		apiYaml := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
  - url: http://localhost:8681
x-weos-config:
  databases:
    - name: Default
      driver: sqlite3
      database: test.db
    - name: Second
      driver: sqlite3
      database: test.db
`
		api, err := rest.New(apiYaml)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		if _, err = api.GetDBConnection("Default"); err != nil {
			t.Errorf("expected a connection '%s' to be created", "Default")
		}
		if _, err = api.GetGormDBConnection("Default"); err != nil {
			t.Errorf("expected a gorm connection '%s' to be created", "Default")
		}
		if _, err = api.GetDBConnection("Second"); err != nil {
			t.Errorf("expected a gorm connection '%s' to be created", "Second")
		}
		if _, err = api.GetGormDBConnection("Second"); err != nil {
			t.Errorf("expected a gorm connection '%s' to be created", "Second")
		}

	})
}

func TestDefaultProjection(t *testing.T) {
	t.Run("legacy database config", func(t *testing.T) {
		apiYaml := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
  - url: http://localhost:8681
x-weos-config:
  database:
    driver: sqlite3
    database: test.db
`
		api, err := rest.New(apiYaml)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultProjection(context.TODO(), api, api.Swagger)
		if _, err = api.GetProjection("Default"); err != nil {
			t.Fatalf("expected a projection to be created")
		}

	})

	t.Run("no config no errors", func(t *testing.T) {
		apiYaml := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
  - url: http://localhost:8681
`
		api, err := rest.New(apiYaml)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultProjection(context.TODO(), api, api.Swagger)
		if err != nil {
			t.Errorf("unexpected error instantiating '%s'", err)
		}
	})

	t.Run("multiple sql database connections", func(t *testing.T) {
		apiYaml := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
  - url: http://localhost:8681
x-weos-config:
  databases:
    - name: Default
      driver: sqlite3
      database: test.db
    - name: Second
      driver: sqlite3
      database: test.db
`
		api, err := rest.New(apiYaml)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultProjection(context.TODO(), api, api.Swagger)
		if _, err = api.GetProjection("Default"); err != nil {
			t.Errorf("expected a projection '%s' to be created", "Default")
		}
	})
}

func TestDefaultEventStore(t *testing.T) {
	t.Run("legacy database config", func(t *testing.T) {
		apiYaml := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
  - url: http://localhost:8681
x-weos-config:
  database:
    driver: sqlite3
    database: test.db
`
		api, err := rest.New(apiYaml)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultProjection(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultEventStore(context.TODO(), api, api.Swagger)
		if _, err = api.GetEventStore("Default"); err != nil {
			t.Fatalf("expected a eventstore to be created")
		}

	})

	t.Run("no config no errors", func(t *testing.T) {
		apiYaml := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
  - url: http://localhost:8681
`
		api, err := rest.New(apiYaml)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultProjection(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultEventStore(context.TODO(), api, api.Swagger)
		if err != nil {
			t.Errorf("unexpected error instantiating '%s'", err)
		}
	})

	t.Run("multiple sql database connections", func(t *testing.T) {
		apiYaml := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
  - url: http://localhost:8681
x-weos-config:
  databases:
    - name: Default
      driver: sqlite3
      database: test.db
    - name: Second
      driver: sqlite3
      database: test.db
`
		api, err := rest.New(apiYaml)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultEventStore(context.TODO(), api, api.Swagger)
		//the event store should be set even though there is no default projection
		if _, err = api.GetEventStore("Default"); err != nil {
			t.Errorf("expected a eventstore '%s' to be created, got error '%s'", "Default", err)
		}
	})
}
