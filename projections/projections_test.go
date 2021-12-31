package projections_test

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/gommon/log"
	"github.com/ory/dockertest/v3"
	"github.com/wepala/weos-service/controllers/rest"
	weos "github.com/wepala/weos-service/model"
	"github.com/wepala/weos-service/projections"
)

var db *sql.DB
var driver = flag.String("driver", "sqlite3", "run database tests")
var host = flag.String("host", "localhost", "database host")
var dbuser = flag.String("user", "root", "database user")
var password = flag.String("password", "secret", "database password")
var port = flag.Int("port", 49179, "database port")
var maxOpen = flag.Int("open", 4, "database maximum open connections")
var maxIdle = flag.Int("idle", 1, "database maximum idle connections")
var database = flag.String("database", "", "database name")
var app weos.Service

type dbConfig struct {
	Host     string `json:"host"`
	User     string `json:"username"`
	Password string `json:"password"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Driver   string `json:"driver"`
	MaxOpen  int    `json:"max-open"`
	MaxIdle  int    `json:"max-idle"`
}

func TestMain(t *testing.M) {
	flag.Parse()

	var pool *dockertest.Pool
	var resource *dockertest.Resource
	var err error

	config := dbConfig{
		Host:     *host,
		User:     *dbuser,
		Password: *password,
		Port:     *port,
		Database: *database,
		Driver:   *driver,
		MaxOpen:  *maxOpen,
		MaxIdle:  *maxIdle,
	}
	switch *driver {
	case "postgres":
		log.Info("Started postgres database")
		// uses a sensible default on windows (tcp/http) and linux/osx (socket)
		pool, err = dockertest.NewPool("")
		if err != nil {
			log.Fatalf("Could not connect to docker: %s", err.Error())
		}

		// pulls an image, creates a container based on it and runs it
		resource, err = pool.Run("postgres", "10.7", []string{"POSTGRES_USER=root", "POSTGRES_PASSWORD=secret", "POSTGRES_DB=test"})
		if err != nil {
			log.Fatalf("Could not start resource: %s", err.Error())
		}

		// exponential backoff-retry, because the module in the container might not be ready to accept connections yet
		if err = pool.Retry(func() error {
			db, err = sql.Open("postgres", fmt.Sprintf("host=localhost port=%s user=root password=secret sslmode=disable database=test", resource.GetPort("5432/tcp")))
			//db, err = pgx.Connect(context.Background(),fmt.Sprintf("host=localhost port=%s user=root password=secret sslmode=disable database=test", resource.GetPort("5432/tcp")))
			if err != nil {
				return err
			}
			return db.Ping()
		}); err != nil {
			log.Fatalf("Could not connect to docker: %s", err.Error())
		}

		connection := strings.Split(resource.GetHostPort("5432/tcp"), ":")

		config.Host = connection[0]
		config.Port, _ = strconv.Atoi(connection[1])
	case "mysql":
		log.Info("Started mysql database")
		// uses a sensible default on windows (tcp/http) and linux/osx (socket)
		pool, err = dockertest.NewPool("")
		if err != nil {
			log.Fatalf("Could not connect to docker: %s", err)
		}

		// pulls an image, creates a container based on it and runs it
		resource, err = pool.Run("mysql", "5.7", []string{"MYSQL_ROOT_PASSWORD=secret"})
		if err != nil {
			log.Fatalf("Could not start resource: %s", err)
		}

		// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
		if err = pool.Retry(func() error {
			db, err = sql.Open("mysql", fmt.Sprintf("root:secret@(localhost:%s)/mysql?sql_mode='ERROR_FOR_DIVISION_BY_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION'&parseTime=%s", resource.GetPort("3306/tcp"), strconv.FormatBool(true)))
			if err != nil {
				return err
			}
			return db.Ping()
		}); err != nil {
			log.Fatalf("Could not connect to docker: %s", err)
		}
		connection := strings.Split(resource.GetHostPort("3306/tcp"), ":")
		config.Host = connection[0]
		config.Port, _ = strconv.Atoi(connection[1])
	case "sqlite3":
		log.Infof("Started sqlite3 database")
		db, err = sql.Open(*driver, "projection.db")
		if err != nil {
			log.Fatalf("failed to create sqlite database '%s'", err)
		}
	}
	defer db.Close()
	appConfig := &weos.ServiceConfig{
		ModuleID: "123",
		Title:    "Test App",
		Database: &weos.DBConfig{
			Driver:   config.Driver,
			Host:     config.Host,
			User:     config.User,
			Password: config.Password,
			Database: config.Database,
			Port:     config.Port,
		},
		Log:           nil,
		ApplicationID: "12345",
	}

	app, err = weos.NewApplicationFromConfig(appConfig, nil, db, nil, nil)
	if err != nil {
		log.Fatalf("failed to set up projections test '%s'", err)
	}

	code := t.Run()

	switch *driver {
	case "postgres", "mysql":
		// You can't defer this because os.Exit doesn't care for defer
		err = pool.Purge(resource)
		if err != nil {
			log.Fatalf("Could not purge resource: %s", err.Error())
		}
	}

	os.Remove("test.db")
	os.Remove("projection.db")

	os.Exit(code)
}

func TestProjections_InitilizeBasicTable(t *testing.T) {

	t.Run("Create basic table with no speecified primary key", func(t *testing.T) {
		openAPI := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
x-weos-config:
  logger:
    level: warn
    report-caller: true
    formatter: json
  database:
    driver: sqlite3
    database: test.db
  event-source:
    - title: default
      driver: service
      endpoint: https://prod1.weos.sh/events/v1
    - title: event
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
    Blog:
     type: object
     properties:
       title:
         type: string
         description: blog title
       description:
         type: string
`

		loader := openapi3.NewSwaggerLoader()
		swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
		if err != nil {
			t.Fatal(err)
		}

		schemes := rest.CreateSchema(context.Background(), nil, swagger)
		p, err := projections.NewProjection(context.Background(), app, schemes)
		if err != nil {
			t.Fatal(err)
		}

		err = p.Migrate(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		gormDB := app.DB()
		if !gormDB.Migrator().HasTable("Blog") {
			t.Fatal("expected to get a table 'Blog'")
		}

		columns, _ := gormDB.Migrator().ColumnTypes("Blog")

		found := false
		found1 := false
		found2 := false
		for _, c := range columns {
			if c.Name() == "id" {
				found = true
			}
			if c.Name() == "title" {
				found1 = true
			}
			if c.Name() == "description" {
				found2 = true
			}
		}

		if !found1 || !found2 || !found {
			t.Fatal("not all fields found")
		}

		gormDB.Table("Blog").Create(map[string]interface{}{"title": "hugs"})
		result := []map[string]interface{}{}
		gormDB.Table("Blog").Find(&result)

		gormDB.Migrator().DropTable("Blog")

		//check for auto id
		if result[0]["id"].(int64) != 1 {
			t.Fatalf("expected an automatic id of '%d' to be set, got '%d'", 1, result[0]["id"])
		}
	})

	t.Run("Create basic table with speecified primary key", func(t *testing.T) {
		openAPI := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
x-weos-config:
  logger:
    level: warn
    report-caller: true
    formatter: json
  database:
    driver: sqlite3
    database: test.db
  event-source:
    - title: default
      driver: service
      endpoint: https://prod1.weos.sh/events/v1
    - title: event
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
    Blog:
     type: object
     properties:
       guid:
         type: string
       title:
         type: string
         description: blog title
       description:
         type: string
     x-identifier:
       - guid
`

		loader := openapi3.NewSwaggerLoader()
		swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
		if err != nil {
			t.Fatal(err)
		}

		schemes := rest.CreateSchema(context.Background(), nil, swagger)
		p, err := projections.NewProjection(context.Background(), app, schemes)
		if err != nil {
			t.Fatal(err)
		}

		err = p.Migrate(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		gormDB := app.DB()
		if !gormDB.Migrator().HasTable("Blog") {
			t.Fatal("expected to get a table 'Blog'")
		}

		columns, _ := gormDB.Migrator().ColumnTypes("Blog")

		found := false
		found1 := false
		found2 := false
		for _, c := range columns {
			if c.Name() == "guid" {
				found = true
			}
			if c.Name() == "title" {
				found1 = true
			}
			if c.Name() == "description" {
				found2 = true
			}
		}

		if !found1 || !found2 || !found {
			t.Fatal("not all fields found")
		}

		tresult := gormDB.Table("Blog").Create(map[string]interface{}{"title": "hugs"})
		if tresult.Error == nil {
			t.Errorf("expected an error because the primary key was not set")
		}

		result := []map[string]interface{}{}
		gormDB.Table("Blog").Find(&result)
		if len(result) != 0 {
			t.Fatal("expectedd no blogs to be created with a missing id field")
		}

		gormDB.Migrator().DropTable("Blog")
	})
}

func TestProjections_InitializeCompositeKeyTable(t *testing.T) {
}
