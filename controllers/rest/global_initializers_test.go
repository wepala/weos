package rest_test

import (
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"testing"
)

func TestGlobalMiddlewareInitializer(t *testing.T) {
	api, err := rest.New("./fixtures/blog-security.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}

	t.Run("auth middleware was added to context", func(t *testing.T) {
		ctxt, err := rest.Security(context.TODO(), api, api.Swagger)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		middlewares := rest.GetOperationMiddlewares(ctxt)
		if len(middlewares) != 1 {
			t.Fatalf("expected the middlewares in context to be %d, got %d", 1, len(middlewares))
		}
	})
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

	t.Run("custom eventstore set", func(t *testing.T) {
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
		api.RegisterProjection("SomeProjection", &ProjectionMock{
			GetEventHandlerFunc: func() model.EventHandler {
				return func(ctx context.Context, event model.Event) error {
					return nil
				}
			},
		})
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultProjection(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultEventStore(context.TODO(), api, api.Swagger)
		if err != nil {
			t.Fatalf("unexpected error setting up default event store '%s'", err)
		}
		//the event store should be set even though there is no default projection
		var eventStore model.EventRepository
		if eventStore, err = api.GetEventStore("Default"); err != nil {
			t.Fatalf("expected a eventstore '%s' to be created, got error '%s'", "Default", err)
		}
		qrProjection, err := api.GetProjection("SomeProjection")
		eventStore.AddSubscriber(qrProjection.GetEventHandler())
		subscribers, err := eventStore.GetSubscribers()
		if err != nil {
			t.Fatalf("unexpected error retrieving subscribers '%s'", err)
		}
		if len(subscribers) != 1 {
			t.Errorf("expected %d subscribers, got %d", 1, len(subscribers))
		}
	})
}

func TestRegisterEntityRepositories(t *testing.T) {
	t.Run("register entity repositories for ", func(t *testing.T) {
		api, err := rest.New("./fixtures/blog.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		api.RegisterLog("Default", &LogMock{})
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultProjection(context.TODO(), api, api.Swagger)
		_, err = rest.RegisterEntityRepositories(context.TODO(), api, api.Swagger)
		if err != nil {
			t.Fatalf("unexpected error registering entity repositories '%s'", err)
		}
		_, err = api.GetEntityRepository("Blog")
		if err != nil {
			t.Errorf("expected a entity repository '%s' to be created, got error '%s'", "Blog", err)
		}
		_, err = api.GetEntityRepository("Post")
		if err != nil {
			t.Errorf("expected a entity repository '%s' to be created, got error '%s'", "Post", err)
		}
		_, err = api.GetEntityRepository("Author")
		if err != nil {
			t.Errorf("expected a entity repository '%s' to be created, got error '%s'", "Author", err)
		}
	})

	t.Run("skip x-inline schemas", func(t *testing.T) {
		api, err := rest.New("./fixtures/fhir.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		api.RegisterLog("Default", &LogMock{})
		_, err = rest.SQLDatabase(context.TODO(), api, api.Swagger)
		_, err = rest.DefaultProjection(context.TODO(), api, api.Swagger)
		_, err = rest.RegisterEntityRepositories(context.TODO(), api, api.Swagger)
		if err != nil {
			t.Fatalf("unexpected error registering entity repositories '%s'", err)
		}
		_, err = api.GetEntityRepository("Address")
		if err == nil {
			t.Errorf("expected inline schema to be skipped, got no error")
		}
	})
}
