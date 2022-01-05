package rest_test

import (
	"database/sql"
	"flag"
	weos "github.com/wepala/weos-service/model"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	dynamicstruct "github.com/ompluscator/dynamic-struct"
	"github.com/wepala/weos-service/controllers/rest"
	"github.com/wepala/weos-service/projections"
	"golang.org/x/net/context"
)

func TestCreateSchema(t *testing.T) {
	t.Run("table name is set correctly", func(t *testing.T) {
		content, err := ioutil.ReadFile("./fixtures/blog.yaml")
		if err != nil {
			t.Fatalf("error loading api specification '%s'", err)
		}
		//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
		tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
		//replace environment variables in file
		tempFile = os.ExpandEnv(string(tempFile))
		tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
		//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
		re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
		tempFile = re.ReplaceAllString(tempFile, `:$1`)
		content = []byte(tempFile)
		loader := openapi3.NewSwaggerLoader()
		swagger, err := loader.LoadSwaggerFromData(content)
		if err != nil {
			t.Fatalf("error loading api specification '%s'", err)
		}
		//instantiate api
		e := echo.New()

		result := rest.CreateSchema(context.Background(), e, swagger)
		//loop through and confirm each has a table name set
		for tableName, table := range result {
			reader := dynamicstruct.NewReader(table)
			if reader.GetField("Table") == nil {
				t.Fatalf("expected a table field")
			}
			if reader.GetField("Table").String() != tableName {
				t.Errorf("There was an error setting the table name, expected '%s'", tableName)
			}
		}

		//check for foreign key on Post table to Author
		postTable, ok := result["Post"]
		if !ok {
			t.Fatalf("expected to find a table Post")
		}

		reader := dynamicstruct.NewReader(postTable)
		if !reader.HasField("AuthorId") {
			t.Errorf("expected the struct to have field '%s'", "AuthorId")
		}

		if !reader.HasField("AuthorEmail") {
			t.Errorf("expected the struct to have field '%s'", "AuthorEmail")
		}
	})
}

var db *sql.DB
var driver = flag.String("driver", "sqlite3", "run database tests")
var host = flag.String("host", "localhost", "database host")
var dbuser = flag.String("user", "root", "database user")
var password = flag.String("password", "secret", "database password")
var port = flag.Int("port", 49179, "database port")
var maxOpen = flag.Int("open", 4, "database maximum open connections")
var maxIdle = flag.Int("idle", 1, "database maximum idle connections")
var database = flag.String("database", "", "database name")

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

func TestCreateSchema_RequiredField(t *testing.T) {

	content, err := ioutil.ReadFile("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
	tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
	//replace environment variables in file
	tempFile = os.ExpandEnv(string(tempFile))
	tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	tempFile = re.ReplaceAllString(tempFile, `:$1`)
	content = []byte(tempFile)
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData(content)
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	//open db
	e := echo.New()
	db, err = sql.Open(*driver, "test_schema.db")
	if err != nil {
		t.Errorf("unexpected error '%s'", err)
	}
	defer db.Close()
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
	//create a service
	app, err := weos.NewApplicationFromConfig(appConfig, nil, db, nil, nil)
	if err != nil {
		t.Fatalf("failed to set up middleware initialize test '%s'", err)
	}
	result := rest.CreateSchema(context.Background(), e, swagger)
	//create a gorm projection
	gormProject, err := projections.NewProjection(context.Background(), app, result)
	if err != nil {
		t.Fatalf("failed to set up projection '%s'", err)
	}
	err = gormProject.Migrate(context.Background())
	if err != nil {
		t.Fatalf("failed to migrate db '%s'", err)
	}

	t.Run("Create basic entity without a required fields", func(t *testing.T) {
		payload := map[string]interface{}{"description": "This is a second blog", "url": "www.testblog.com"}
		db1 := app.DB().Table("Blog").Create(payload)
		if db1.Error == nil {
			t.Fatalf("expected an error got nil")
		}
	})
}
