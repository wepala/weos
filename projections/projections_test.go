package projections_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/segmentio/ksuid"
	weosContext "github.com/wepala/weos-service/context"
	"gorm.io/gorm/clause"

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
			db, err = sql.Open("mysql", fmt.Sprintf("root:secret@(localhost:%s)/mysql?sql_mode='ERROR_FOR_DIVISION_BY_ZERO,STRICT_TRANS_TABLES,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION'&parseTime=%s", resource.GetPort("3306/tcp"), strconv.FormatBool(true)))
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
		db.Exec("PRAGMA foreign_keys = ON")
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

	os.Remove("./projection.db")
	os.Exit(code)
}

func TestProjections_InitilizeBasicTable(t *testing.T) {

	t.Run("Create basic table with no specified primary key", func(t *testing.T) {
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
    Post:
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

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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
			t.Errorf("expected to get a table 'Blog'")
		}

		if !gormDB.Migrator().HasTable("Post") {
			t.Errorf("expected to get a table 'Post'")
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

		//check for auto id
		if *driver != "mysql" {
			if result[0]["id"].(int64) != 1 {
				t.Fatalf("expected an automatic id of '%d' to be set, got '%d'", 1, result[0]["id"])
			}
		} else {
			if result[0]["id"].(uint64) != 1 {
				t.Fatalf("expected an automatic id of '%d' to be set, got '%d'", 1, result[0]["id"])
			}
		}

		//automigrate table again to ensure no issue on multiple migrates
		for i := 0; i < 10; i++ {
			_, err = projections.NewProjection(context.Background(), app, schemes)
			if err != nil {
				t.Fatal(err)
			}

			//testing number of tables would not work in mysql since it can create many auxilliary tables
			if !gormDB.Migrator().HasTable("Blog") {
				t.Errorf("expected to get a table 'Blog'")
			}

			if !gormDB.Migrator().HasTable("Post") {
				t.Errorf("expected to get a table 'Post'")
			}

		}

		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}

		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
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

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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

		tresult := gormDB.Table("Blog").Create(map[string]interface{}{"title": "hugs2"})
		if tresult.Error == nil {
			t.Errorf("expected an error because the primary key was not set")
		}

		result := []map[string]interface{}{}
		gormDB.Table("Blog").Find(&result)
		if len(result) != 0 {
			t.Fatal("expected no blogs to be created with a missing id field")
		}

		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
		}
	})
}

func TestProjections_InitializeCompositeKeyTable(t *testing.T) {
}

func TestProjections_CreateBasicRelationship(t *testing.T) {

	t.Run("Create basic many to one relationship", func(t *testing.T) {
		openAPI := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
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
    Post:
     type: object
     properties:
      title:
         type: string
         description: blog title
      description:
         type: string
      blog:
         $ref: "#/components/schemas/Blog"
`

		loader := openapi3.NewSwaggerLoader()
		swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
		if err != nil {
			t.Fatal(err)
		}

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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
			t.Errorf("expected to get a table 'Blog'")
		}

		if !gormDB.Migrator().HasTable("Post") {
			t.Errorf("expected to get a table 'Post'")
		}

		columns, _ := gormDB.Migrator().ColumnTypes("Post")

		found := false
		found1 := false
		found2 := false
		found3 := false
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
			if c.Name() == "blog_id" {
				found3 = true
			}
		}

		if !found1 || !found2 || !found || !found3 {
			t.Fatal("not all fields found")
		}

		gormDB.Table("Blog").Create(map[string]interface{}{"title": "hugs"})
		result := gormDB.Table("Post").Create(map[string]interface{}{"title": "hugs", "blog_id": 1})
		if result.Error != nil {
			t.Errorf("expected to create a post with relationship, got err '%s'", result.Error)
		}

		result = gormDB.Table("Post").Create(map[string]interface{}{"title": "hugs"})
		if result.Error != nil {
			t.Errorf("expected to create a post without relationship, got err '%s'", result.Error)
		}

		result = gormDB.Table("Post").Create(map[string]interface{}{"title": "hugs", "blog_id": 5})
		if result.Error == nil {
			t.Errorf("expected to be unable to create post with invalid reference to blog")
		}

		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
		}
	})

	t.Run("Create basic many to many relationship", func(t *testing.T) {
		openAPI := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
components:
  schemas:
    Post:
     type: object
     properties:
      title:
         type: string
         description: blog title
      description:
         type: string
    Blog:
     type: object
     properties:
       title:
         type: string
         description: blog title
       description:
         type: string
       posts:
        type: array
        items:
          $ref: "#/components/schemas/Post"
`

		loader := openapi3.NewSwaggerLoader()
		swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
		if err != nil {
			t.Fatal(err)
		}

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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
			t.Errorf("expected to get a table 'Blog'")
		}

		if !gormDB.Migrator().HasTable("Post") {
			t.Errorf("expected to get a table 'Post'")
		}

		if !gormDB.Migrator().HasTable("blog_posts") {
			t.Errorf("expected to get a table 'blog_posts'")
		}

		columns, _ := gormDB.Migrator().ColumnTypes("blog_posts")

		found := false
		found1 := false
		for _, c := range columns {
			if c.Name() == "id" {
				found = true
			}
			if c.Name() == "post_id" {
				found1 = true
			}
		}

		if !found1 || !found {
			t.Fatal("not all fields found")
		}
		gormDB.Table("Post").Create(map[string]interface{}{"title": "hugs"})
		gormDB.Table("Blog").Create(map[string]interface{}{"title": "hugs"})
		result := gormDB.Table("blog_posts").Create(map[string]interface{}{
			"id":      1,
			"post_id": 1,
		})
		if result.Error != nil {
			t.Errorf("expected to create a post with relationship, got err '%s'", result.Error)
		}

		result = gormDB.Table("blog_posts").Create(map[string]interface{}{
			"id":      1,
			"post_id": 5,
		})
		if result.Error == nil {
			t.Errorf("expected to be unable to create a relationship without an valis post id")
		}

		//automigrate table again to ensure no issue on multiple migrates
		for i := 0; i < 20; i++ {
			_, err = projections.NewProjection(context.Background(), app, schemes)
			if err != nil {
				t.Fatal(err)
			}

			//testing number of tables would not work in mysql since it can create many auxilliary tables
			if !gormDB.Migrator().HasTable("Blog") {
				t.Errorf("expected to get a table 'Blog'")
			}

			if !gormDB.Migrator().HasTable("Post") {
				t.Errorf("expected to get a table 'Post'")
			}

			if !gormDB.Migrator().HasTable("blog_posts") {
				t.Errorf("expected to get a table 'blog_posts'")
			}

		}

		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
		}
	})
}

func TestProjections_Create(t *testing.T) {

	t.Run("Basic Create", func(t *testing.T) {
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

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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

		payload := map[string]interface{}{"weos_id": "123456", "title": "testBlog", "description": "This is a create projection test"}
		contentEntity := &weos.ContentEntity{
			AggregateRoot: weos.AggregateRoot{
				BasicEntity: weos.BasicEntity{
					ID: "123456",
				},
			},
			Property: payload,
		}

		ctxt := context.Background()
		ctxt = context.WithValue(ctxt, weosContext.CONTENT_TYPE, &weosContext.ContentType{
			Name: "Blog",
		})

		event := weos.NewEntityEvent("create", contentEntity, contentEntity.ID, &payload)
		contentEntity.NewChange(event)
		p.GetEventHandler()(ctxt, *event)

		blog := map[string]interface{}{}
		result := gormDB.Table("Blog").Find(&blog, "weos_id = ? ", contentEntity.ID)
		if result.Error != nil {
			t.Fatalf("unexpected error retreiving created blog '%s'", result.Error)
		}

		if blog["title"] != payload["title"] {
			t.Fatalf("expected title to be %s, got %s", payload["title"], blog["title"])
		}

		if blog["description"] != payload["description"] {
			t.Fatalf("expected desription to be %s, got %s", payload["desription"], blog["desription"])
		}

		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
	})

	t.Run("Create with many to one relationship", func(t *testing.T) {
		openAPI := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
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
    Post:
     type: object
     properties:
      title:
         type: string
         description: blog title
      description:
         type: string
      blog:
         $ref: "#/components/schemas/Blog"
`

		loader := openapi3.NewSwaggerLoader()
		swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
		if err != nil {
			t.Fatal(err)
		}

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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

		payload := map[string]interface{}{"weos_id": "123456", "title": "testBlog", "description": "This is a create projection test"}
		contentEntity := &weos.ContentEntity{
			AggregateRoot: weos.AggregateRoot{
				BasicEntity: weos.BasicEntity{
					ID: "123456",
				},
			},
			Property: payload,
		}

		ctxt := context.Background()
		ctxt = context.WithValue(ctxt, weosContext.CONTENT_TYPE, &weosContext.ContentType{
			Name: "Blog",
		})

		event := weos.NewEntityEvent("create", contentEntity, contentEntity.ID, &payload)
		contentEntity.NewChange(event)
		p.GetEventHandler()(ctxt, *event)

		//create post
		payload = map[string]interface{}{"weos_id": "1234567", "title": "testPost", "description": "This is a create projection test", "blog_id": 1}
		contentEntity = &weos.ContentEntity{
			AggregateRoot: weos.AggregateRoot{
				BasicEntity: weos.BasicEntity{
					ID: "1234567",
				},
			},
			Property: payload,
		}

		ctxt = context.Background()
		ctxt = context.WithValue(ctxt, weosContext.CONTENT_TYPE, &weosContext.ContentType{
			Name: "Post",
		})

		event = weos.NewEntityEvent("create", contentEntity, contentEntity.ID, &payload)
		contentEntity.NewChange(event)
		p.GetEventHandler()(ctxt, *event)

		post := map[string]interface{}{}
		result := gormDB.Table("Post").Find(&post, "weos_id = ? ", contentEntity.ID)
		if result.Error != nil {
			t.Fatalf("unexpected error retreiving created blog '%s'", result.Error)
		}

		if post["title"] != payload["title"] {
			t.Fatalf("expected title to be %s, got %s", payload["title"], post["title"])
		}

		if post["description"] != payload["description"] {
			t.Fatalf("expected desription to be %s, got %s", payload["desription"], post["desription"])
		}

		if fmt.Sprint(post["blog_id"]) != fmt.Sprint(payload["blog_id"]) {
			t.Fatalf("expected desription to be %d, got %d", payload["blog_id"], post["blog_id"])
		}

		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
	})
}

func TestProjections_Update(t *testing.T) {

	t.Run("Basic Update", func(t *testing.T) {
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

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
		p, err := projections.NewProjection(context.Background(), app, schemes)
		if err != nil {
			t.Fatal(err)
		}

		err = p.Migrate(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		gormDB := app.DB()

		id := ksuid.New().String()
		gormDB.Table("Blog").Create(map[string]interface{}{"weos_id": id, "title": "hugs"})

		payload := map[string]interface{}{"id": 1, "title": "testBlog", "description": "This is a create projection test"}
		contentEntity := &weos.ContentEntity{
			AggregateRoot: weos.AggregateRoot{
				BasicEntity: weos.BasicEntity{
					ID: id,
				},
			},
			Property: payload,
		}

		ctxt := context.Background()
		ctxt = context.WithValue(ctxt, weosContext.CONTENT_TYPE, &weosContext.ContentType{
			Name: "Blog",
		})

		event := weos.NewEntityEvent("update", contentEntity, contentEntity.ID, &payload)
		p.GetEventHandler()(ctxt, *event)

		blog := map[string]interface{}{}
		result := gormDB.Table("Blog").Find(&blog, "weos_id = ? ", contentEntity.ID)
		if result.Error != nil {
			t.Fatalf("unexpected error retreiving created blog '%s'", result.Error)
		}

		if blog["title"] != payload["title"] {
			t.Fatalf("expected title to be %s, got %s", payload["title"], blog["title"])
		}

		if blog["description"] != payload["description"] {
			t.Fatalf("expected desription to be %s, got %s", payload["desription"], blog["desription"])
		}

		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
	})

	t.Run("Update basic many to many relationship", func(t *testing.T) {
		openAPI := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
components:
  schemas:
    Post:
     type: object
     properties:
      title:
         type: string
         description: blog title
      description:
         type: string
    Blog:
     type: object
     properties:
       title:
         type: string
         description: blog title
       description:
         type: string
       posts:
        type: array
        items:
          $ref: "#/components/schemas/Post"
`

		loader := openapi3.NewSwaggerLoader()
		swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
		if err != nil {
			t.Fatal(err)
		}

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
		p, err := projections.NewProjection(context.Background(), app, schemes)
		if err != nil {
			t.Fatal(err)
		}

		err = p.Migrate(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		gormDB := app.DB()

		blogWeosID := ksuid.New().String()
		postWeosID := ksuid.New().String()
		postWeosID2 := ksuid.New().String()
		gormDB.Table("Post").Create(map[string]interface{}{"weos_id": postWeosID, "title": "hugs"})
		gormDB.Table("Post").Create(map[string]interface{}{"weos_id": postWeosID2, "title": "hugs"})
		gormDB.Table("Blog").Create(map[string]interface{}{"weos_id": blogWeosID, "title": "hugs"})

		payload := map[string]interface{}{"weos_id": blogWeosID, "id": 1, "title": "testBlog", "description": "This is a create projection test", "posts": []map[string]interface{}{
			{
				"id": 1,
			},
		}}
		contentEntity := &weos.ContentEntity{
			AggregateRoot: weos.AggregateRoot{
				BasicEntity: weos.BasicEntity{
					ID: blogWeosID,
				},
			},
			Property: payload,
		}

		ctxt := context.Background()
		ctxt = context.WithValue(ctxt, weosContext.CONTENT_TYPE, &weosContext.ContentType{
			Name: "Blog",
		})

		event := weos.NewEntityEvent("update", contentEntity, contentEntity.ID, &payload)
		p.GetEventHandler()(ctxt, *event)

		blog := map[string]interface{}{}
		result := gormDB.Table("Blog").Find(&blog, "weos_id = ? ", contentEntity.ID)
		if result.Error != nil {
			t.Fatalf("unexpected error retreiving created blog '%s'", result.Error)
		}

		if blog["title"] != payload["title"] {
			t.Fatalf("expected title to be %s, got %s", payload["title"], blog["title"])
		}

		if blog["description"] != payload["description"] {
			t.Fatalf("expected desription to be %s, got %s", payload["desription"], blog["desription"])
		}

		blogpost := map[string]interface{}{}
		result = gormDB.Table("blog_posts").Find(&blogpost, "id = ? ", 1)
		if result.Error != nil {
			t.Errorf("unexpected error retreiving created blog post relation '%s'", result.Error)
		}

		if *driver == "mysql" {
			id := blogpost["post_id"].(uint64)
			if id != 1 {
				t.Errorf("expected post id to be %d, got %v", 1, blogpost["post_id"])
			}
		} else {
			if blogpost["post_id"] != int64(1) {
				t.Errorf("expected post id to be %d, got %v", 1, blogpost["post_id"])
			}
		}

		//test replace associations
		payload = map[string]interface{}{"id": 1, "weos_id": blogWeosID, "title": "testBlog", "description": "This is a create projection test", "posts": []map[string]interface{}{
			{
				"id": 2,
			},
		}}
		contentEntity = &weos.ContentEntity{
			AggregateRoot: weos.AggregateRoot{
				BasicEntity: weos.BasicEntity{
					ID: blogWeosID,
				},
			},
			Property: payload,
		}

		ctxt = context.Background()
		ctxt = context.WithValue(ctxt, weosContext.CONTENT_TYPE, &weosContext.ContentType{
			Name: "Blog",
		})

		event = weos.NewEntityEvent("update", contentEntity, contentEntity.ID, &payload)
		p.GetEventHandler()(ctxt, *event)

		blogposts := []map[string]interface{}{}
		result = gormDB.Table("blog_posts").Find(&blogposts, "id = ? ", 1)
		if result.Error != nil {
			t.Fatalf("unexpected error retreiving created blog post relation '%s'", result.Error)
		}

		if len(blogposts) != 1 {
			t.Fatalf("expected there to be %d blog posts got %v", 1, len(blogposts))
		}
		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}

		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}

		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}

	})
}

func TestProjections_GetContentTypeByEntityID(t *testing.T) {
	openAPI := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
components:
  schemas:
    Post:
     type: object
     properties:
      title:
         type: string
         description: blog title
      description:
         type: string
    Blog:
     type: object
     properties:
       title:
         type: string
         description: blog title
       description:
         type: string
       posts:
        type: array
        items:
          $ref: "#/components/schemas/Post"
`

	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
	if err != nil {
		t.Fatal(err)
	}

	schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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
		t.Errorf("expected to get a table 'Blog'")
	}

	if !gormDB.Migrator().HasTable("Post") {
		t.Errorf("expected to get a table 'Post'")
	}

	if !gormDB.Migrator().HasTable("blog_posts") {
		t.Errorf("expected to get a table 'blog_posts'")
	}

	columns, _ := gormDB.Migrator().ColumnTypes("blog_posts")

	found := false
	found1 := false
	for _, c := range columns {
		if c.Name() == "id" {
			found = true
		}
		if c.Name() == "post_id" {
			found1 = true
		}
	}

	if !found1 || !found {
		t.Fatal("not all fields found")
	}
	gormDB.Table("Post").Create(map[string]interface{}{"weos_id": "1234", "sequence_no": 1, "title": "punches"})
	gormDB.Table("Blog").Create(map[string]interface{}{"weos_id": "5678", "sequence_no": 1, "title": "hugs"})
	result := gormDB.Table("blog_posts").Create(map[string]interface{}{
		"id":      1,
		"post_id": 1,
	})
	if result.Error != nil {
		t.Errorf("expected to create a post with relationship, got err '%s'", result.Error)
	}

	r, err := p.GetByEntityID(context.Background(), weosContext.ContentType{Name: "Blog"}, "5678")
	if err != nil {
		t.Fatalf("error querying '%s' '%s'", "Blog", err)
	}
	if r["title"] != "hugs" {
		t.Errorf("expected the blog title to be %s got %v", "hugs", r["titles"])
	}

	if *driver != "sqlite3" {
		posts := r["posts"].([]interface{})
		if len(posts) != 1 {
			t.Errorf("expected to get %d posts, got %d", 1, len(posts))
		}

		pp := posts[0].(map[string]interface{})
		if pp["title"] != "punches" {
			t.Errorf("expected the post title to be %s got %v", "punches", pp["title"])
		}

		if id, ok := pp["weos_id"]; ok {
			if id != "" {
				t.Errorf("there should be no weos_id value")
			}
		}

		if no, ok := pp["sequence_no"]; ok {
			if no != 0 {
				t.Errorf("there should be no sequence number value")
			}
		}
	}

	err = gormDB.Migrator().DropTable("blog_posts")
	if err != nil {
		t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
	}

	err = gormDB.Migrator().DropTable("Blog")
	if err != nil {
		t.Errorf("error removing table '%s' '%s'", "Blog", err)
	}
	err = gormDB.Migrator().DropTable("Post")
	if err != nil {
		t.Errorf("error removing table '%s' '%s'", "Post", err)
	}
}

func TestProjections_GetContentTypeByKeys(t *testing.T) {
	openAPI := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
components:
  schemas:
    Post:
     type: object
     properties:
      title:
         type: string
         description: blog title
      description:
         type: string
    Blog:
     type: object
     properties:
       title:
         type: string
         description: blog title
       author_id:
         type: string
       description:
         type: string
       posts:
        type: array
        items:
          $ref: "#/components/schemas/Post"
     x-identifier:
      - title
      - author_id
`

	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
	if err != nil {
		t.Fatal(err)
	}

	schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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
		t.Errorf("expected to get a table 'Blog'")
	}

	if !gormDB.Migrator().HasTable("Post") {
		t.Errorf("expected to get a table 'Post'")
	}

	if !gormDB.Migrator().HasTable("blog_posts") {
		t.Errorf("expected to get a table 'blog_posts'")
	}

	columns, _ := gormDB.Migrator().ColumnTypes("blog_posts")

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
		if c.Name() == "author_id" {
			found2 = true
		}
	}

	if !found1 || !found || !found2 {
		t.Fatal("not all fields found")
	}
	gormDB.Table("Post").Create(map[string]interface{}{"weos_id": "1234", "sequence_no": 1, "title": "punches"})
	gormDB.Table("Blog").Create(map[string]interface{}{"weos_id": "5678", "sequence_no": 1, "title": "hugs", "author_id": "kidding"})
	gormDB.Table("Blog").Create(map[string]interface{}{"weos_id": "9101", "sequence_no": 1, "title": "hugs 2 - the reckoning", "author_id": "kidding"})
	result := gormDB.Table("blog_posts").Create(map[string]interface{}{
		"author_id": "kidding",
		"title":     "hugs",
		"id":        1,
	})
	if result.Error != nil {
		t.Errorf("expected to create a post with relationship, got err '%s'", result.Error)
	}

	blogRef := swagger.Components.Schemas["Blog"]
	r, err := p.GetByKey(context.Background(), weosContext.ContentType{Name: "Blog", Schema: blogRef.Value}, map[string]interface{}{
		"author_id": "kidding",
		"title":     "hugs",
	})
	if err != nil {
		t.Fatalf("error querying '%s' '%s'", "Blog", err)
	}

	if r["title"] != "hugs" {
		t.Errorf("expected the blog title to be %s got %v", "hugs", r["titles"])
	}

	if *driver != "sqlite3" {
		posts, ok := r["posts"].([]interface{})
		if !ok {
			t.Fatal("expected to get a posts array")
		}
		if len(posts) != 1 {
			t.Errorf("expected to get %d posts, got %d", 1, len(posts))
		}

		pp := posts[0].(map[string]interface{})
		if pp["title"] != "punches" {
			t.Errorf("expected the post title to be %s got %v", "punches", pp["title"])
		}

		if id, ok := pp["weos_id"]; ok {
			if id != "" {
				t.Errorf("there should be no weos_id value")
			}
		}

		if no, ok := pp["sequence_no"]; ok {
			if no != 0 {
				t.Errorf("there should be no sequence number value")
			}
		}
	}
	err = gormDB.Migrator().DropTable("blog_posts")
	if err != nil {
		t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
	}

	err = gormDB.Migrator().DropTable("Blog")
	if err != nil {
		t.Errorf("error removing table '%s' '%s'", "Blog", err)
	}
	err = gormDB.Migrator().DropTable("Post")
	if err != nil {
		t.Errorf("error removing table '%s' '%s'", "Post", err)
	}
}

func TestProjections_GormOperations(t *testing.T) {
	t.Run("Basic Create using schema", func(t *testing.T) {
		openAPI := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
components:
  schemas:
    Post:
     type: object
     properties:
      title:
         type: string
         description: blog title
      description:
         type: string
    Blog:
     type: object
     properties:
       title:
         type: string
         description: blog title
       description:
         type: string
       posts:
        type: array
        items:
          $ref: "#/components/schemas/Post"
`

		loader := openapi3.NewSwaggerLoader()
		swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
		if err != nil {
			t.Fatal(err)
		}

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
		p, err := projections.NewProjection(context.Background(), app, schemes)
		if err != nil {
			t.Fatal(err)
		}

		err = p.Migrate(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		gormDB := app.DB()

		m := map[string]interface{}{"table_alias": "Blog", "title": "hugs", "posts": []map[string]interface{}{
			{
				"id": 1,
			},
		}}

		bytes, _ := json.Marshal(m)

		blog := schemes["Blog"]

		json.Unmarshal(bytes, &blog)

		gormDB.Table("Post").Create(map[string]interface{}{"title": "hugs"})

		//create without table reference fails
		//the dynamic struct entity is a pointer so just place it directly into the function
		//using gormDB.Model will fail since the struct isn't recognized.  But it unmarshalls correctly which is what matters
		result := gormDB.Table("Blog").Create(blog)
		if result.Error != nil {
			t.Errorf("got error creating blog %s", result.Error)
		}

		post := schemes["Post"]
		m = map[string]interface{}{"title": "hills"}
		bytes, _ = json.Marshal(m)
		json.Unmarshal(bytes, &post)
		result = gormDB.Table("Post").Create(post)
		if result.Error != nil {
			t.Errorf("got error creating post %s", result.Error)
		}

		m = map[string]interface{}{"id": 1, "table_alias": "Blog", "title": "hugs", "posts": []map[string]interface{}{
			{
				"id": 1,
			},
			{
				"id": 2,
			},
		}}
		bytes, _ = json.Marshal(m)
		json.Unmarshal(bytes, &blog)

		result = gormDB.Table("Blog").Where("id = ?", 1).Updates(blog)
		if result.Error != nil {
			t.Errorf("got error updating blog %s", result.Error)
		}

		m = map[string]interface{}{"id": 1, "table_alias": "Blog", "title": "hugs"}
		bytes, _ = json.Marshal(m)
		json.Unmarshal(bytes, &blog)

		result = gormDB.Table("Blog").Updates(blog)
		if result.Error != nil {
			t.Errorf("got error updating blog %s", result.Error)
		}

		r := map[string]interface{}{"id": 1}

		//first works only when a model struct is specified
		gormDB.Table("Blog").Take(&r)
		fmt.Printf("retrieved blog is %v", r)

		r = map[string]interface{}{"id": 1, "table_alias": "Blog"}
		bytes, _ = json.Marshal(r)
		json.Unmarshal(bytes, &blog)

		//not getting the association values
		gormDB.Preload(clause.Associations).Take(blog)
		reader := ds.NewReader(blog)
		fmt.Printf("\nretrieved blog is %v\n", reader.GetAllFields())

		gormDB.Preload(clause.Associations).Take(&r)
		fmt.Printf("\nretrieved blog is %v\n", r)

		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
		}
	})
}

func TestProjections_Create_With_Required(t *testing.T) {
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
     required:
       - title
`

	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
	if err != nil {
		t.Fatal(err)
	}

	schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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
			nullable, _ := c.Nullable()
			if nullable {
				t.Errorf("expected the title field to be NOT nullable")
			}
		}
		if c.Name() == "description" {
			found2 = true
			if gormDB.Dialector.Name() != "sqlite" { // for the nullable check to work the sql driver needs to support it
				nullable, _ := c.Nullable()
				if !nullable {
					t.Errorf("expected the description field to be nullable by default")
				}
			}

		}
	}

	if !found1 || !found2 || !found {
		t.Fatal("not all fields found")
	}

	t.Run("can't create without required field", func(t *testing.T) {
		mockWeOSID := "adsf123"
		blog := map[string]interface{}{"weos_id": mockWeOSID, "description": "This is a create projection test"}
		gormDB.Table("Blog").Create(blog)
		var blogResult map[string]interface{}
		result := gormDB.Table("Blog").Find(&blogResult, "weos_id = ? ", mockWeOSID)
		if result.RowsAffected > 0 {
			t.Errorf("expected error since the title field is required")
		}
	})

	t.Run("create without description", func(t *testing.T) {
		mockWeOSID := "adsf456"
		blog := map[string]interface{}{"weos_id": mockWeOSID, "title": "This is a create projection test"}
		gormDB.Table("Blog").Create(blog)
		var blogResult map[string]interface{}
		result := gormDB.Table("Blog").Find(&blogResult, "weos_id = ? ", mockWeOSID)
		if result.RowsAffected != 1 {
			t.Errorf("expected %d result for id '%s', got %d", 1, mockWeOSID, result.RowsAffected)
		}
	})

	err = gormDB.Migrator().DropTable("Blog")
	if err != nil {
		t.Fatal("error cleaning up after test")
	}

}

func TestProjections_GetContentEntity(t *testing.T) {

	t.Run("Get a created entity", func(t *testing.T) {
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

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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

		payload := map[string]interface{}{"weos_id": "123456", "title": "testBlog", "description": "This is a create projection test"}
		contentEntity := &weos.ContentEntity{
			AggregateRoot: weos.AggregateRoot{
				BasicEntity: weos.BasicEntity{
					ID: "123456",
				},
			},
			Property: payload,
		}

		ctxt := context.Background()
		for name, scheme := range swagger.Components.Schemas {
			ctxt = context.WithValue(ctxt, weosContext.CONTENT_TYPE, &weosContext.ContentType{
				Name:   strings.Title(name),
				Schema: scheme.Value,
			})
		}

		event := weos.NewEntityEvent("create", contentEntity, contentEntity.ID, &payload)
		contentEntity.NewChange(event)
		p.GetEventHandler()(ctxt, *event)

		blog, err := p.GetContentEntity(ctxt, contentEntity.ID)
		if err != nil {
			t.Errorf("Error getting content type: got %s", err)
		}

		if blog.GetString("Title") != payload["title"] {
			t.Fatalf("expected title to be %s, got %s", payload["title"], blog.GetString("title"))
		}

		if blog.GetString("Description") != payload["description"] {
			t.Fatalf("expected desription to be %s, got %s", payload["desription"], blog.GetString("description"))
		}

		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
	})
}

func TestProjections_Nullable(t *testing.T) {

	t.Run("Check if a column is nullable", func(t *testing.T) {
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
     required:
       - title
`
		loader := openapi3.NewSwaggerLoader()
		swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
		if err != nil {
			t.Fatal(err)
		}

		schemes := rest.CreateSchema(context.Background(), echo.New(), swagger)
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

		for _, c := range columns {
			if *driver != "sqlite3" {
				if c.Name() == "title" {
					columnNullable, _ := c.Nullable()
					if !strings.EqualFold(strconv.FormatBool(columnNullable), "false") {
						t.Fatalf("expected the title nullable state to be %s, got %s", "false", strconv.FormatBool(columnNullable))
					}
				}
				if c.Name() == "description" {
					columnNullable, _ := c.Nullable()
					if !strings.EqualFold(strconv.FormatBool(columnNullable), "true") {
						t.Fatalf("expected the description nullable state to be %s, got %s", "true", strconv.FormatBool(columnNullable))
					}
				}
			}
		}

		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
	})
}
