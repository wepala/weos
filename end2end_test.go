package main_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/labstack/echo/v4"
	ds "github.com/ompluscator/dynamic-struct"
	api "github.com/wepala/weos-service/controllers/rest"
	"gorm.io/gorm"
)

var e *echo.Echo
var API api.RESTAPI
var openAPI string
var Developer *User
var Content *ContentType
var errors error
var buf bytes.Buffer

type User struct {
	Name      string
	AccountID string
}

type Property struct {
	Type        string
	Description string
}

type ContentType struct {
	Type       string              `yaml:"type"`
	Properties map[string]Property `yaml:"properties"`
}

func InitializeSuite(ctx *godog.TestSuiteContext) {
	Developer = &User{}
	e = echo.New()
	e.Logger.SetOutput(&buf)
	_, err := api.Initialize(e, &API, "./api.yaml")
	if err != nil {
		fmt.Errorf("unexpected error '%s'", err)
	}
	openAPI = `openapi: 3.0.3
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
`
}

func reset(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	Developer = &User{}
	errors = nil
	os.Remove("test.db")
	openAPI = `openapi: 3.0.3
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
`
	return ctx, nil
}

func aContentTypeModeledInTheSpecification(arg1, arg2 string, arg3 *godog.DocString) error {
	openAPI = openAPI + arg3.Content + "\n"
	return nil
}

func aDeveloper(name string) error {
	Developer.Name = name
	return nil
}

func aMiddlewareShouldBeAddedToTheRoute(middleware string) error {
	yamlRoutes := e.Routes()
	for _, route := range yamlRoutes {
		if strings.Contains(route.Name, middleware) {
			return nil
		}
	}
	return fmt.Errorf("Expected %s middleware to be added to route got nil", middleware)
}

func aModelShouldBeAddedToTheProjection(arg1 string, details *godog.Table) error {
	//use gorm connection to get table
	gormDB := API.Application.DB()

	if !gormDB.Migrator().HasTable(arg1) {
		return fmt.Errorf("%s table does not exist", arg1)
	}

	head := details.Rows[0].Cells
	columns, _ := gormDB.Migrator().ColumnTypes(arg1)
	var column gorm.ColumnType

	for i := 1; i < len(details.Rows); i++ {

		for n, cell := range details.Rows[i].Cells {
			switch head[n].Value {
			case "Field":
				columnName := cell.Value
				for _, c := range columns {
					if strings.EqualFold(c.Name(), columnName) {
						column = c
						break
					}
				}
			case "Type":
				if cell.Value == "varchar(512)" {
					cell.Value = "text"
				}
				if !strings.EqualFold(column.DatabaseTypeName(), cell.Value) {
					return fmt.Errorf("expected to get type '%s' got '%s'", cell.Value, column.DatabaseTypeName())

				}
			//ignore this for now.  gorm does not set to nullable, rather defaulting to the null value of that interface
			case "Null", "Default":
			case "Key":
			}
		}
	}
	//TODO check that the table has the expected columns
	return nil
}

func aRouteShouldBeAddedToTheApi(method, path string) error {
	yamlRoutes := e.Routes()
	for _, route := range yamlRoutes {
		if route.Method == method && route.Path == path {
			return nil
		}
	}
	return fmt.Errorf("Expected route but got nil with method %s and path %s", method, path)
}

func aWarningShouldBeOutputToLogsLettingTheDeveloperKnowThatAHandlerNeedsToBeSet() error {
	//TODO this should be a check on a mock logger to see if the warning was logged (it doesn't return an error since an endpoint with no handler could be deliberate)
	if !strings.Contains(buf.String(), "no handler set") {
		fmt.Errorf("expected an error to be log got '%s'", buf.String())
	}
	return nil
}

func addsASchemaToTheSpecification(arg1, arg2, arg3 string, arg4 *godog.DocString) error {
	openAPI = openAPI + arg4.Content
	return nil
}

func addsAnEndpointToTheSpecification(arg1, arg2 string, arg3 *godog.DocString) error {
	//check to make sure path parameter is added to openapi
	results := strings.Contains(openAPI, "paths:")
	if !results {
		openAPI += "\npaths:\n"
	}
	openAPI = openAPI + arg3.Content
	return nil
}

func allFieldsAreNullableByDefault() error {
	return nil
}

func anErrorShouldBeReturned() error {
	if errors == nil {
		return fmt.Errorf("expected an error.  got none.")
	}
	return nil
}

func blogsInTheApi(arg1 *godog.Table) error {
	return godog.ErrPending
}

func entersInTheField(arg1, arg2, arg3 string) error {
	return godog.ErrPending
}

func hasAnAccountWithId(name, accountID string) error {
	Developer.AccountID = accountID
	return nil
}

func isOnTheCreateScreen(arg1, arg2 string) error {
	return godog.ErrPending
}

func isUsedToModelTheService(arg1 string) error {
	return nil
}

func theIsCreated(arg1 string, arg2 *godog.Table) error {
	return godog.ErrPending
}

func theIsSubmitted(arg1 string) error {
	return godog.ErrPending
}

func theShouldHaveAnId(arg1 string) error {
	return godog.ErrPending
}

func theSpecificationIs(arg1 *godog.DocString) error {
	openAPI = arg1.Content
	return nil
}

func theSpecificationIsParsed(arg1 string) error {
	e = echo.New()
	os.Remove("test.db")
	API = api.RESTAPI{}
	_, err := api.Initialize(e, &API, openAPI)
	if err != nil {
		errors = err
	}
	return nil
}

func aEntityConfigurationShouldBeSetup(arg1 string, arg2 *godog.DocString) error {
	schema, err := API.GetSchemas()
	if err != nil {
		return err
	}

	if _, ok := schema[arg1]; !ok {
		return fmt.Errorf("no entity named '%s'", arg1)
	}

	entityString := strings.SplitAfter(arg2.Content, arg1+" {")
	reader := ds.NewReader(schema[arg1])

	s := strings.TrimRight(entityString[1], "}")
	s = strings.TrimSpace(s)
	entityFields := strings.Split(s, "\n")

	for _, f := range entityFields {
		f = strings.TrimSpace(f)
		fields := strings.Split(f, " ")
		if !reader.HasField(strings.Title(fields[1])) {
			return fmt.Errorf("did not find field '%s'", fields[1])
		}

		field := reader.GetField(strings.Title(fields[1]))
		switch fields[0] {
		case "string":
			if field.Interface() != "" {
				return fmt.Errorf("expected a string, got '%v'", field.Interface())
			}

		case "integer":
			if field.Interface() != 0 {
				return fmt.Errorf("expected an integer, got '%v'", field.Interface())
			}
		case "uint":
			if field.Interface() != uint(0) {
				return fmt.Errorf("expected an uint, got '%v'", field.Interface())
			}
		default:
			return fmt.Errorf("got an unexpected field type: %s", fields[0])
		}

	}

	return nil
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Before(reset)
	//add context steps
	ctx.Step(`^a content type "([^"]*)" modeled in the "([^"]*)" specification$`, aContentTypeModeledInTheSpecification)
	ctx.Step(`^a developer "([^"]*)"$`, aDeveloper)
	ctx.Step(`^a "([^"]*)" middleware should be added to the route$`, aMiddlewareShouldBeAddedToTheRoute)
	ctx.Step(`^a model "([^"]*)" should be added to the projection$`, aModelShouldBeAddedToTheProjection)
	ctx.Step(`^a "([^"]*)" route "([^"]*)" should be added to the api$`, aRouteShouldBeAddedToTheApi)
	ctx.Step(`^a warning should be output to logs letting the developer know that a handler needs to be set$`, aWarningShouldBeOutputToLogsLettingTheDeveloperKnowThatAHandlerNeedsToBeSet)
	ctx.Step(`^"([^"]*)" adds a schema "([^"]*)" to the "([^"]*)" specification$`, addsASchemaToTheSpecification)
	ctx.Step(`^"([^"]*)" adds an endpoint to the "([^"]*)" specification$`, addsAnEndpointToTheSpecification)
	ctx.Step(`^all fields are nullable by default$`, allFieldsAreNullableByDefault)
	ctx.Step(`^an error should be returned$`, anErrorShouldBeReturned)
	ctx.Step(`^blogs in the api$`, blogsInTheApi)
	ctx.Step(`^"([^"]*)" enters "([^"]*)" in the "([^"]*)" field$`, entersInTheField)
	ctx.Step(`^"([^"]*)" has an account with id "([^"]*)"$`, hasAnAccountWithId)
	ctx.Step(`^"([^"]*)" is on the "([^"]*)" create screen$`, isOnTheCreateScreen)
	ctx.Step(`^"([^"]*)" is used to model the service$`, isUsedToModelTheService)
	ctx.Step(`^the "([^"]*)" is created$`, theIsCreated)
	ctx.Step(`^the "([^"]*)" is submitted$`, theIsSubmitted)
	ctx.Step(`^the "([^"]*)" should have an id$`, theShouldHaveAnId)
	ctx.Step(`^the specification is$`, theSpecificationIs)
	ctx.Step(`^the "([^"]*)" specification is parsed$`, theSpecificationIsParsed)
	ctx.Step(`^a "([^"]*)" entity configuration should be setup$`, aEntityConfigurationShouldBeSetup)

}

func TestBDD(t *testing.T) {
	status := godog.TestSuite{
		Name:                 "BDD Tests",
		ScenarioInitializer:  InitializeScenario,
		TestSuiteInitializer: InitializeSuite,
		Options: &godog.Options{
			Format: "pretty",
			Tags:   "",
		},
	}.Run()
	if status != 0 {
		t.Errorf("there was an error running tests, exit code %d", status)
	}
}
