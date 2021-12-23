package main_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/labstack/echo/v4"
	api "github.com/wepala/weos-service/controllers/rest"
)

var e *echo.Echo
var API api.RESTAPI
var openAPI string
var Developer *User
var Content *ContentType
var errors error
var buf bytes.Buffer
var payload ContentType
var rec *httptest.ResponseRecorder
var createScreen bool
var db *sql.DB
var checkContentID ContentType

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
	payload = ContentType{}
	payload.Properties = map[string]Property{}
	rec = httptest.NewRecorder()
	createScreen = false
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

func aEntityConfigurationShouldBeSetup(arg1 string, arg2 *godog.DocString) error {
	return godog.ErrPending
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

func aModelShouldBeAddedToTheProjection(arg1 string, arg2 *godog.Table) error {
	//use gorm connection to get table
	if !API.Application.DB().Migrator().HasTable("blog") {
		return fmt.Errorf("blog table does not exist")
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
	if rec.Result().StatusCode == http.StatusCreated {
		return fmt.Errorf("expected error but got status '%s'", rec.Result().Status)
	}
	return nil
}

func blogsInTheApi(arg1 *godog.Table) error {
	return godog.ErrPending
}

func entersInTheField(userName, value, field string) error {
	tempProp := Property{
		Type:        field,
		Description: value,
	}
	if payload.Properties == nil {
		payload.Properties = map[string]Property{}
	}
	payload.Properties[field] = tempProp
	return nil
}

func hasAnAccountWithId(name, accountID string) error {
	Developer.AccountID = accountID
	return nil
}

func isOnTheCreateScreen(userName, contentType string) error {
	createScreen = true
	payload.Type = contentType
	return nil
}

func isUsedToModelTheService(arg1 string) error {
	return nil
}

func theIsCreated(contentType string, details *godog.Table) error {
	if rec.Result().StatusCode != http.StatusCreated {
		return fmt.Errorf("expected the status code to be '%d', got '%d'", http.StatusCreated, rec.Result().StatusCode)
	}
	head := details.Rows[0].Cells
	compareContent := ContentType{
		Type: contentType,
	}

	for i := 1; i < len(details.Rows); i++ {
		for n, cell := range details.Rows[i].Cells {
			switch head[n].Value {
			case "title":
				prop := Property{
					Type:        head[n].Value,
					Description: cell.Value,
				}
				compareContent.Properties[head[n].Value] = prop
			case "description":
				prop := Property{
					Type:        head[n].Value,
					Description: cell.Value,
				}
				compareContent.Properties[head[n].Value] = prop
			}
		}
	}
	contentTypes := []ContentType{}
	found := 0
	API.Application.DB().Find(contentTypes)
	for _, content := range contentTypes {
		for _, prop := range content.Properties {
			if prop.Description == compareContent.Properties[prop.Type].Description {
				found++
				if found == len(compareContent.Properties) {
					checkContentID = content
				}
				continue
			}
		}
	}
	if found != len(compareContent.Properties) {
		return fmt.Errorf("unexpected error did not find %s created", contentType)
	}
	return nil
}

func theIsSubmitted(contentType string) error {
	if createScreen {
		reqBytes, _ := json.Marshal(payload)
		body := bytes.NewReader(reqBytes)
		request := httptest.NewRequest("POST", "/"+contentType, body)
		request = request.WithContext(context.TODO())
		request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		request.Close = true
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, request)
	}
	return nil
}

func theShouldHaveAnId(contentType string) error {
	if checkContentID.Properties["id"].Description == "" {
		return fmt.Errorf("expected to find an id got nil for %s ", contentType)
	}
	return nil
}

func theSpecificationIs(arg1 *godog.DocString) error {
	openAPI = arg1.Content
	e = echo.New()
	os.Remove("test.db")
	API = api.RESTAPI{}
	_, err := api.Initialize(e, &API, openAPI)
	if err != nil {
		return err
	}
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

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Before(reset)
	//add context steps
	ctx.Step(`^a content type "([^"]*)" modeled in the "([^"]*)" specification$`, aContentTypeModeledInTheSpecification)
	ctx.Step(`^a developer "([^"]*)"$`, aDeveloper)
	ctx.Step(`^a "([^"]*)" entity configuration should be setup$`, aEntityConfigurationShouldBeSetup)
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

}

func TestBDD(t *testing.T) {
	status := godog.TestSuite{
		Name:                 "BDD Tests",
		ScenarioInitializer:  InitializeScenario,
		TestSuiteInitializer: InitializeSuite,
		Options: &godog.Options{
			Format: "pretty",
			Tags:   "WEOS-1130",
		},
	}.Run()
	if status != 0 {
		t.Errorf("there was an error running tests, exit code %d", status)
	}
}
