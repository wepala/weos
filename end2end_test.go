package main_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	weosContext "github.com/wepala/weos/context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	api "github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"github.com/wepala/weos/utils"
	"gorm.io/gorm"
)

var e *echo.Echo
var API api.RESTAPI
var openAPI string
var Developer *User
var Content *ContentType
var errs error
var buf bytes.Buffer
var payload ContentType
var rec *httptest.ResponseRecorder
var header http.Header
var resp *http.Response
var db *sql.DB
var gormDB *gorm.DB
var dbconfig dbConfig
var requests map[string]map[string]interface{}
var responseBody map[string]interface{}
var currScreen string
var contentTypeID map[string]bool
var dockerEndpoint string
var reqBody string
var imageName string
var binary string
var dockerFile string
var binaryMount string
var esContainer testcontainers.Container
var limit int
var page int
var contentType string
var result api.ListApiResponse
var scenarioContext context.Context
var blogfixtures []interface{}
var total int
var success int
var failed int
var errArray []error
var filters string
var token string

type FilterProperties struct {
	Operator string
	Field    string
	Value    string
	Values   []string
}
type User struct {
	Name      string
	AccountID string
}

type Property struct {
	Type        string
	Description string
}

type Blog struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ContentType struct {
	Type       string              `yaml:"type"`
	Properties map[string]Property `yaml:"properties"`
}

func InitializeSuite(ctx *godog.TestSuiteContext) {
	requests = map[string]map[string]interface{}{}
	contentTypeID = map[string]bool{}
	Developer = &User{}
	filters = ""
	page = 1
	limit = 0
	token = ""
	result = api.ListApiResponse{}
	blogfixtures = []interface{}{}
	total, success, failed = 0, 0, 0
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
    database: "%s"
    driver: "%s"
    host: "%s"
    password: "%s"
    username: "%s"
    port: %d
  event-source:
    - title: default
      driver: service
      endpoint: https://prod1.weos.sh/events/v1
    - title: event
      driver: sqlite3
      database: e2e.db
  databases:
    - title: default
      driver: sqlite3
      database: e2e.db
  rest:
    middleware:
      - RequestID
      - Recover
      - ZapLogger
components:
  schemas:
`
	openAPI = fmt.Sprintf(openAPI, dbconfig.Database, dbconfig.Driver, dbconfig.Host, dbconfig.Password, dbconfig.User, dbconfig.Port)
}

func reset(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	scenarioContext = context.Background()
	requests = map[string]map[string]interface{}{}
	contentTypeID = map[string]bool{}
	Developer = &User{}
	filters = ""
	page = 1
	limit = 0
	token = ""
	result = api.ListApiResponse{}
	errs = nil
	header = make(http.Header)
	rec = httptest.NewRecorder()
	resp = nil
	blogfixtures = []interface{}{}
	total, success, failed = 0, 0, 0
	e = echo.New()
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
    database: "%s"
    driver: "%s"
    host: "%s"
    password: "%s"
    username: "%s"
    port: %d
  event-source:
    - title: default
      driver: service
      endpoint: https://prod1.weos.sh/events/v1
    - title: event
      driver: sqlite3
      database: e2e.db
  databases:
    - title: default
      driver: sqlite3
      database: e2e.db
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

func dropDB() error {

	var errr error
	if *driver == "sqlite3" {
		os.Remove("e2e.db")
		db, errr = sql.Open("sqlite3", "e2e.db")
	} else if *driver == "postgres" {
		r := gormDB.Exec(`DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;`)
		errr = r.Error
	} else if *driver == "mysql" {
		_, r := db.Exec(`drop DATABASE IF EXISTS mysql;`)
		if r != nil {
			return r
		}
		_, r = db.Exec(`create DATABASE IF NOT EXISTS mysql;`)
		errr = r
	}
	return errr
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
	apiProjection, err := API.GetProjection("Default")
	if err != nil {
		return fmt.Errorf("unexpected error getting projection: %s", err)
	}
	apiProjection1 := apiProjection.(*projections.GORMDB)
	gormDB := apiProjection1.DB()

	if !gormDB.Migrator().HasTable(arg1) {
		arg1 = utils.SnakeCase(arg1)
		if !gormDB.Migrator().HasTable(arg1) {
			return fmt.Errorf("%s table does not exist", arg1)
		}
	}

	head := details.Rows[0].Cells
	columns, _ := gormDB.Migrator().ColumnTypes(arg1)
	var column gorm.ColumnType

	for i := 1; i < len(details.Rows); i++ {
		payload := map[string]interface{}{}
		keys := []string{}
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
					if *driver == "sqlite3" {
						cell.Value = "text"
					} else {
						cell.Value = "varchar"
					}
				}

				if cell.Value == "integer" {
					if *driver == "postgres" {
						cell.Value = "int8"
					}
					if *driver == "mysql" {
						cell.Value = "bigint"
					}
				}

				if cell.Value == "datetime" {
					if *driver == "postgres" {
						cell.Value = "timestamptz"
					}
				}
				if !strings.EqualFold(column.DatabaseTypeName(), cell.Value) {
					if cell.Value == "varchar" && *driver == "postgres" {
						//string values for postgres can be both text and varchar
						if !strings.EqualFold(column.DatabaseTypeName(), "text") {
							return fmt.Errorf("expected to get type '%s' got '%s'", "text", column.DatabaseTypeName())
						}
					} else if cell.Value == "varchar" && *driver == "mysql" {
						//string values for postgres can be both text and varchar
						if !strings.EqualFold(column.DatabaseTypeName(), "longtext") {
							return fmt.Errorf("expected to get type '%s' got '%s'", "longtext", column.DatabaseTypeName())
						}
					} else {
						return fmt.Errorf("expected to get type '%s' got '%s'", cell.Value, column.DatabaseTypeName())
					}
				}
			//ignore this for now.  gorm does not set to nullable, rather defaulting to the null value of that interface
			case "Null", "Default":

			case "Key":
				if strings.EqualFold(cell.Value, "pk") {
					if !strings.EqualFold(column.Name(), "id") { //default id tag
						if _, ok := payload["id"]; ok {
							payload["id"] = nil
						}
					}
					keys = append(keys, cell.Value)
				}
			}
		}
		if len(keys) > 1 && !strings.EqualFold(keys[0], "id") {
			resultDB := gormDB.Table(arg1).Create(payload)
			if resultDB.Error == nil {
				return fmt.Errorf("expected a missing primary key error")
			}
		}
	}
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

func aRouteShouldBeAddedToTheApi1(method string) error {
	yamlRoutes := e.Routes()
	for _, route := range yamlRoutes {
		if route.Method == method {
			return nil
		}
	}
	return fmt.Errorf("Expected route but got nil with method %s", method)
}

func aWarningShouldBeOutputToLogsLettingTheDeveloperKnowThatAHandlerNeedsToBeSet() error {
	if !strings.Contains(buf.String(), "no handler set") {
		return fmt.Errorf("expected an error to be log got '%s'", buf.String())
	}
	return nil
}

func aWarningShouldBeOutputToLogsLettingTheDeveloperKnowThatAParameterForEachPartOfTheIdenfierMustBeSet() error {
	if !strings.Contains(buf.String(), "a parameter for each part of the identifier must be set") {
		return fmt.Errorf("expected an error to be log got '%s'", buf.String())
	}
	return nil
}

func aWarningShouldBeOutputBecauseTheEndpointIsInvalid() error {
	if !strings.Contains(buf.String(), "no handler set") {
		return fmt.Errorf("expected an error to be log got '%s'", buf.String())
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
	if rec.Result().StatusCode == http.StatusCreated && errs == nil {
		return fmt.Errorf("expected error but got status '%s'", rec.Result().Status)
	}
	return nil
}

func blogsInTheApi(details *godog.Table) error {

	head := details.Rows[0].Cells

	for i := 1; i < len(details.Rows); i++ {
		req := make(map[string]interface{})
		for n, cell := range details.Rows[i].Cells {
			req[head[n].Value] = cell.Value
		}

		blogfixtures = append(blogfixtures, req)

	}

	return nil
}

func entersInTheField(userName, value, field string) error {

	requests[currScreen][strings.ToLower(field)] = value
	return nil
}

func hasAnAccountWithId(name, accountID string) error {
	Developer.AccountID = accountID
	return nil
}

func isOnTheCreateScreen(userName, contentType string) error {

	requests[strings.ToLower(contentType+"_create")] = map[string]interface{}{}
	currScreen = strings.ToLower(contentType + "_create")
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
	compare := map[string]interface{}{}

	for i := 1; i < len(details.Rows); i++ {
		for n, cell := range details.Rows[i].Cells {
			compare[head[n].Value] = cell.Value
		}
	}

	contentEntity := map[string]interface{}{}
	var result *gorm.DB
	//ETag would help with this
	for key, value := range compare {
		result = gormDB.Table(strings.Title(contentType)).Find(&contentEntity, key+" = ?", value)
		if contentEntity != nil {
			break
		}
	}

	if contentEntity == nil {
		return fmt.Errorf("unexpected error finding content type in db")
	}

	if result.Error != nil {
		return fmt.Errorf("unexpected error finding content type: %s", result.Error)
	}

	for key, value := range compare {
		if contentEntity[key] != value {
			return fmt.Errorf("expected %s %s %s, got %s", contentType, key, value, contentEntity[key])
		}
	}

	contentTypeID[strings.ToLower(contentType)] = true
	return nil
}

func theIsSubmitted(contentType string) error {
	//Used to store the key/value pairs passed in the scenario
	req := make(map[string]interface{})
	for key, value := range requests[currScreen] {
		req[key] = value
	}

	reqBytes, _ := json.Marshal(req)
	body := bytes.NewReader(reqBytes)
	var request *http.Request
	if strings.Contains(currScreen, "create") {
		request = httptest.NewRequest("POST", "/"+strings.ToLower(contentType), body)
	} else if strings.Contains(currScreen, "update") {
		request = httptest.NewRequest("PUT", "/"+strings.ToLower(contentType)+"s/"+fmt.Sprint(req["id"]), body)
	} else if strings.Contains(currScreen, "delete") {
		request = httptest.NewRequest("DELETE", "/"+strings.ToLower(contentType)+"s/"+fmt.Sprint(req["id"]), nil)
	}
	request = request.WithContext(context.TODO())
	header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	header.Set(weosContext.AUTHORIZATION, "Bearer "+token)
	request.Header = header
	request.Close = true
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, request)
	return nil
}

func theShouldHaveAnId(contentType string) error {

	if !contentTypeID[strings.ToLower(contentType)] {
		return fmt.Errorf("expected the " + contentType + " to have an ID")
	}
	return nil
}

func theSpecificationIs(arg1 *godog.DocString) error {
	openAPI = arg1.Content
	return nil
}

func theSpecificationIsParsed(arg1 string) error {
	openAPI = fmt.Sprintf(openAPI, dbconfig.Database, dbconfig.Driver, dbconfig.Host, dbconfig.Password, dbconfig.User, dbconfig.Port)
	tapi, err := api.New(openAPI)
	if err != nil {
		return err
	}
	tapi.DB = db
	API = *tapi
	e = API.EchoInstance()
	buf = bytes.Buffer{}
	e.Logger.SetOutput(&buf)
	err = API.Initialize(scenarioContext)
	if err != nil {
		return err
	}
	proj, err := API.GetProjection("Default")
	if err == nil {
		p := proj.(*projections.GORMDB)
		if p != nil {
			gormDB = p.DB()
		}
	}
	if err != nil {
		return err
	}
	return nil
}

func aEntityConfigurationShouldBeSetup(arg1 string, arg2 *godog.DocString) error {
	schema := API.Schemas
	if _, ok := schema[arg1]; !ok {
		return fmt.Errorf("no entity named '%s'", arg1)
	}

	entityString := strings.SplitAfter(arg2.Content, arg1+" {")
	reader := ds.NewReader(schema[arg1].Build().New())

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
			if field.Interface() != "" && field.Interface() != field.PointerString() {
				return fmt.Errorf("expected a string, got '%v'", field.Interface())
			}

		case "integer":
			if field.Interface() != 0 && field.Interface() != field.PointerInt() {
				return fmt.Errorf("expected an integer, got '%v'", field.Interface())
			}
		case "uint":
			if field.Interface() != uint(0) && field.Interface() != field.PointerUint() {
				return fmt.Errorf("expected an uint, got '%v'", field.Interface())
			}
		case "datetime":
			dateTime := field.Time()
			if dateTime != *new(time.Time) {
				fmt.Printf("date interface is '%v'", field.Interface())
				fmt.Printf("empty date interface is '%v'", new(time.Time))
				return fmt.Errorf("expected an uint, got '%v'", field.Interface())
			}
		default:
			return fmt.Errorf("got an unexpected field type: %s", fields[0])
		}

	}

	return nil
}

func aHeaderWithValue(key, value string) error {
	header.Add(key, value)
	return nil
}

func aResponseShouldBeReturned(statusCode int) error {
	//check resp first
	if resp != nil && resp.StatusCode != statusCode {
		return fmt.Errorf("expected the status code to be '%d', got '%d'", statusCode, resp.StatusCode)
	} else if rec != nil && rec.Result().StatusCode != statusCode {
		return fmt.Errorf("expected the status code to be '%d', got '%d'", statusCode, rec.Result().StatusCode)
	}
	return nil
}

func requestBody(arg1 *godog.DocString) error {
	reqBody = arg1.Content
	return nil
}

func thatTheBinaryIsGenerated(arg1 string) error {
	binary = arg1
	//check if the binary exists and if not throw an error
	if _, err := os.Stat("./" + binary); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("weos binary not found")
	}
	return nil
}

func theBinaryIsRunWithTheSpecification() error {
	binaryPath, err := filepath.Abs("./" + binary)
	if err != nil {
		return err
	}
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        imageName,
		Name:         "BDDTest",
		ExposedPorts: []string{"8681/tcp"},
		BindMounts:   map[string]string{binaryMount: binaryPath},
		Entrypoint:   []string{binaryMount},
		//Entrypoint: []string{"tail", "-f", "/dev/null"},
		Env:        map[string]string{"WEOS_SPEC": openAPI},
		WaitingFor: wait.ForLog("started"),
	}
	esContainer, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return fmt.Errorf("unexpected error starting container '%s'", err)
	}

	//get the endpoint that the container was run on
	var endpoint string
	endpoint, err = esContainer.Host(ctx) //didn't use the endpoint call because it returns "localhost" which the client doesn't seem to like
	cport, err := esContainer.MappedPort(ctx, "8681")
	if err != nil {
		return fmt.Errorf("error setting up container '%s'", err)
	}
	dockerEndpoint = "http://" + endpoint + ":" + cport.Port()
	return nil
}

func isRunOnTheOperatingSystemAs(arg1 string, arg2 string) error {
	imageName = arg1
	binaryMount = arg2
	return nil
}

func theEndpointIsHit(method, contentType string) error {
	if binary != "" {
		reqBytes, _ := json.Marshal(reqBody)
		body := bytes.NewReader(reqBytes)
		request := httptest.NewRequest(method, dockerEndpoint+contentType, body)
		request = request.WithContext(context.TODO())
		request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		request.Close = true
		client := http.Client{}
		resp, errs = client.Do(request)
		defer esContainer.Terminate(context.Background())
	} else {
		request := httptest.NewRequest(method, contentType, nil)
		request = request.WithContext(context.TODO())
		header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		header.Set(weosContext.AUTHORIZATION, "Bearer "+token)
		request.Header = header
		request.Close = true
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, request)
	}
	return nil
}

func theServiceIsRunning() error {
	buf = bytes.Buffer{}
	openAPI = fmt.Sprintf(openAPI, dbconfig.Database, dbconfig.Driver, dbconfig.Host, dbconfig.Password, dbconfig.User, dbconfig.Port)
	tapi, err := api.New(openAPI)
	if err != nil {
		return err
	}
	tapi.DB = db
	tapi.EchoInstance().Logger.SetOutput(&buf)
	API = *tapi
	err = API.Initialize(scenarioContext)
	if err != nil {
		return err
	}
	proj, err := API.GetProjection("Default")
	if err == nil {
		p := proj.(*projections.GORMDB)
		if p != nil {
			gormDB = p.DB()
		}
	}
	e = API.EchoInstance()

	if len(blogfixtures) != 0 {
		for _, r := range blogfixtures {
			req := r.(map[string]interface{})
			sequence := req["sequence_no"]
			delete(req, "sequence_no")
			reqBytes, _ := json.Marshal(req)
			body := bytes.NewReader(reqBytes)
			var request *http.Request
			seq := 0
			if _, ok := sequence.(string); ok {
				seq, _ = strconv.Atoi(sequence.(string))
			}
			request = httptest.NewRequest("POST", "/blog", body)

			request = request.WithContext(context.TODO())
			header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			header.Set(weosContext.AUTHORIZATION, "Bearer "+token)
			request.Header = header
			request.Close = true
			rec = httptest.NewRecorder()
			e.ServeHTTP(rec, request)
			if rec.Code != http.StatusCreated {
				return fmt.Errorf("expected the status to be %d got %d", http.StatusCreated, rec.Code)
			}

			if seq > 1 {

				for i := 1; i < seq; i++ {
					reqBytes, _ := json.Marshal(req)
					body := bytes.NewReader(reqBytes)
					request = httptest.NewRequest("PUT", "/blogs/"+req["id"].(string), body)
					request = request.WithContext(context.TODO())
					header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
					header.Set(weosContext.AUTHORIZATION, "Bearer "+token)
					request.Header = header
					request.Close = true
					rec = httptest.NewRecorder()
					e.ServeHTTP(rec, request)
					if rec.Code != http.StatusOK {
						return fmt.Errorf("expected the status to be %d got %d", http.StatusOK, rec.Code)
					}
				}
			}

		}
	}
	token = ""
	return nil
}

func isOnTheEditScreenWithId(user, contentType, id string) error {
	requests[strings.ToLower(contentType+"_update")] = map[string]interface{}{}
	currScreen = strings.ToLower(contentType + "_update")
	requests[currScreen]["id"] = id
	return nil
}

func theHeaderShouldBe(key, value string) error {
	if key == "ETag" {
		Etag := rec.Result().Header.Get(key)
		idEtag, seqNoEtag := api.SplitEtag(Etag)
		if Etag == "" {
			return fmt.Errorf("expected the Etag to be added to header, got %s", Etag)
		}
		if idEtag == "" {
			return fmt.Errorf("expected the Etag to contain a weos id, got %s", idEtag)
		}
		if seqNoEtag == "" {
			return fmt.Errorf("expected the Etag to contain a sequence no, got %s", seqNoEtag)
		}

		if seqNoEtag != strings.Split(value, ".")[1] {
			return fmt.Errorf("expected the Etag to contain a sequence no %s, got %s", strings.Split(value, ".")[1], seqNoEtag)
		}
		return nil
	}

	headers := rec.Result().Header
	val := []string{}

	for k, v := range headers {
		if strings.EqualFold(k, key) {
			val = v
			break
		}
	}

	if len(val) > 0 {
		if strings.EqualFold(val[0], value) {
			return nil
		}
	}
	return fmt.Errorf("expected the header %s value to be %s got %v", key, value, val)
}

func theIsUpdated(contentType string, details *godog.Table) error {
	if rec.Result().StatusCode != http.StatusOK {
		return fmt.Errorf("expected the status code to be '%d', got '%d'", http.StatusOK, rec.Result().StatusCode)
	}

	head := details.Rows[0].Cells
	compare := map[string]interface{}{}

	for i := 1; i < len(details.Rows); i++ {
		for n, cell := range details.Rows[i].Cells {
			compare[head[n].Value] = cell.Value
		}
	}

	contentEntity := map[string]interface{}{}
	var result *gorm.DB
	//ETag would help with this
	for key, value := range compare {
		apiProjection, err := API.GetProjection("Default")
		if err != nil {
			return fmt.Errorf("unexpected error getting projection: %s", err)
		}
		apiProjection1 := apiProjection.(*projections.GORMDB)
		result = apiProjection1.DB().Table(strings.Title(contentType)).Find(&contentEntity, key+" = ?", value)
		if contentEntity != nil {
			break
		}
	}
	if contentEntity == nil {
		result = API.Application.DB().Table(strings.Title(contentType)).Find(&contentEntity, "id = ?", requests[currScreen])
	}

	if contentEntity == nil {
		return fmt.Errorf("unexpected error finding content type in db")
	}

	if result.Error != nil {
		return fmt.Errorf("unexpected error finding content type: %s", result.Error)
	}

	for key, value := range compare {
		if contentEntity[key] != value {
			return fmt.Errorf("expected %s %s %s, got %s", contentType, key, value, contentEntity[key])
		}
	}

	contentTypeID[strings.ToLower(contentType)] = true
	return nil
}

func aBlogShouldBeReturned(details *godog.Table) error {
	head := details.Rows[0].Cells
	compare := map[string]interface{}{}

	for i := 1; i < len(details.Rows); i++ {
		for n, cell := range details.Rows[i].Cells {
			compare[head[n].Value] = cell.Value
		}
	}

	contentEntity := map[string]interface{}{}
	err := json.NewDecoder(rec.Body).Decode(&contentEntity)

	if err != nil {
		return err
	}

	for key, value := range compare {
		if contentEntity[key] != value {
			return fmt.Errorf("expected %s %s %s, got %s", "Blog", key, value, contentEntity[key])
		}
	}

	responseBody = contentEntity
	return nil
}

func sojournerIsUpdatingWithId(contentType, id string) error {
	requests[strings.ToLower(contentType+"_update")] = map[string]interface{}{"id": id}
	currScreen = strings.ToLower(contentType + "_update")
	return nil
}

func aWarningShouldBeOutputToLogs() error {
	if !strings.Contains(buf.String(), "unexpected error: cannot assign different schemas for different content types") {
		return fmt.Errorf("expected an error to be log got '%s'", buf.String())
	}
	return nil
}

func theFormIsSubmittedWithContentType(contentEntity, contentType string) error {
	//Used to store the key/value pairs passed in the scenario

	switch contentType {
	case "application/x-www-form-urlencoded":

		data := url.Values{}

		req := make(map[string]interface{})
		for key, value := range requests[currScreen] {
			data.Set(key, value.(string))
		}

		body := strings.NewReader(data.Encode())

		var request *http.Request
		if strings.Contains(currScreen, "create") {
			request = httptest.NewRequest("POST", "/"+strings.ToLower(contentEntity), body)
		} else if strings.Contains(currScreen, "update") {
			request = httptest.NewRequest("PUT", "/"+strings.ToLower(contentEntity)+"s/"+fmt.Sprint(req["id"]), body)
		}
		request = request.WithContext(context.TODO())
		header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header = header
		request.Close = true
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, request)
		return nil
	case "multipart/form-data":
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)

		req := make(map[string]interface{})
		for key, value := range requests[currScreen] {
			writer.WriteField(key, value.(string))
		}

		writer.Close()

		var request *http.Request
		if strings.Contains(currScreen, "create") {
			request = httptest.NewRequest("POST", "/"+strings.ToLower(contentEntity), body)
		} else if strings.Contains(currScreen, "update") {
			request = httptest.NewRequest("PUT", "/"+strings.ToLower(contentEntity)+"s/"+fmt.Sprint(req["id"]), body)
		}
		request = request.WithContext(context.TODO())
		header.Set("Content-Type", writer.FormDataContentType())
		request.Header = header
		request.Close = true
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, request)
		return nil
	}
	return fmt.Errorf("This content type is not supported: %s", contentType)
}

func theIsSubmittedWithoutContentType(contentEntity string) error {
	req := make(map[string]interface{})
	for key, value := range requests[currScreen] {
		req[key] = value
	}

	reqBytes, _ := json.Marshal(req)
	body := bytes.NewReader(reqBytes)
	var request *http.Request
	if strings.Contains(currScreen, "create") {
		request = httptest.NewRequest("POST", "/"+strings.ToLower(contentEntity), body)
	} else if strings.Contains(currScreen, "update") {
		request = httptest.NewRequest("PUT", "/"+strings.ToLower(contentEntity)+"s/"+fmt.Sprint(req["id"]), body)
	}
	request = request.WithContext(context.TODO())
	request.Close = true
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, request)
	return nil
}

func theHeaderShouldBePresent(arg1 string) error {
	header := rec.Result().Header.Get(arg1)
	if header == "" {
		return fmt.Errorf("no header found with the name: %s", arg1)
	}
	return nil
}

func isOnTheListScreen(user, content string) error {
	contentType = content
	requests[strings.ToLower(contentType+"_list")] = map[string]interface{}{}
	currScreen = strings.ToLower(contentType + "_list")
	return nil
}

func theItemsPerPageAre(pageLimit int) error {
	limit = pageLimit
	return nil
}

func theListResultsShouldBe(details *godog.Table) error {
	head := details.Rows[0].Cells
	compare := map[string]interface{}{}
	compareArray := []map[string]interface{}{}

	for i := 1; i < len(details.Rows); i++ {
		for n, cell := range details.Rows[i].Cells {
			compare[head[n].Value] = cell.Value
		}
		compareArray = append(compareArray, compare)
		compare = map[string]interface{}{}
	}
	foundItems := 0
	response := rec.Result()
	defer response.Body.Close()
	result.Items = make([]map[string]interface{}, len(compareArray))
	err := json.NewDecoder(response.Body).Decode(&result)
	if err != nil {
		return err
	}
	for i, entity := range compareArray {
		foundEntity := true
		for key, value := range entity {
			if strings.Compare(result.Items[i][key].(string), value.(string)) != 0 {
				foundEntity = false
				break
			}
		}
		if foundEntity {
			foundItems++
		}
	}
	if foundItems != len(compareArray) {
		return fmt.Errorf("expected to find %d, got %d", len(compareArray), foundItems)
	}

	return nil
}

func thePageInTheResultShouldBe(pageResult int) error {
	if result.Page != pageResult {
		return fmt.Errorf("expect page to be %d, got %d", pageResult, result.Page)
	}
	return nil
}

func thePageNoIs(pageNo int) error {
	page = pageNo
	return nil
}

func theSearchButtonIsHit() error {
	var request *http.Request
	request = httptest.NewRequest("GET", "/"+strings.ToLower(contentType)+"?limit="+strconv.Itoa(limit)+"&page="+strconv.Itoa(page)+"&"+filters, nil)
	request = request.WithContext(context.TODO())
	header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	request.Header = header
	request.Close = true
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, request)
	return nil
}

func theTotalResultsShouldBe(totalResult int) error {
	if result.Total != int64(totalResult) {
		return fmt.Errorf("expect page to be %d, got %d", totalResult, result.Total)
	}
	return nil
}

func aWarningShouldBeOutputToTheLogsTellingTheDeveloperThePropertyDoesntExist() error {
	if !strings.Contains(buf.String(), "property does not exist") {
	}
	return nil
}

func addsTheAttributeToTheFieldOnTheContentType(user, attribute, field, contentType string) error {
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
	if err != nil {
		return err
	}

	schemas := swagger.Components.Schemas

	attributes := schemas[contentType].Value.Extensions[attribute]
	deletedFields := []string{}
	bytes, _ := json.Marshal(attributes)
	json.Unmarshal(bytes, &deletedFields)
	deletedFields = append(deletedFields, field)
	schemas[contentType].Value.Extensions[attribute] = deletedFields

	swagger.Components.Schemas = schemas

	bytes, err = swagger.MarshalJSON()
	if err != nil {
		return err
	}
	openAPI = string(bytes)
	return nil
}

func addsTheFieldToTheContentType(user, field, fieldType, contentType string) error {
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
	if err != nil {
		return err
	}

	schemas := swagger.Components.Schemas
	switch fieldType {
	case "string":
		schemas[contentType].Value.Properties[field] = &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				Type: "string",
			},
		}
	default:
		fmt.Errorf("no logic for adding field type %s", fieldType)
	}
	swagger.Components.Schemas = schemas

	bytes, err := swagger.MarshalJSON()
	if err != nil {
		return err
	}
	openAPI = string(bytes)
	return nil
}

func anErrorShouldShowLettingTheDeveloperKnowThatIsPartOfAForeignKeyReference() error {
	if errs == nil {
		fmt.Errorf("expected there to be an error on migrating")
		return fmt.Errorf("expected error on migrating")
	}
	//TODO: add checks fo the speicific error
	return nil
}

func removedTheFieldFromTheContentType(user, field, contentType string) error {
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData([]byte(openAPI))
	if err != nil {
		return err
	}

	schemas := swagger.Components.Schemas

	delete(schemas[contentType].Value.Properties, strings.ToLower(field))

	pks, _ := json.Marshal(schemas[contentType].Value.Extensions["x-identifier"])
	primayKeys := []string{}
	json.Unmarshal(pks, &primayKeys)
	for i, k := range primayKeys {
		if strings.EqualFold(k, field) {
			primayKeys[i] = primayKeys[len(primayKeys)-1]
			primayKeys = primayKeys[:len(primayKeys)-1]
		}
	}

	schemas[contentType].Value.Extensions["x-identifier"] = primayKeys

	swagger.Components.Schemas = schemas

	bytes, err := swagger.MarshalJSON()
	if err != nil {
		return err
	}
	openAPI = string(bytes)
	return nil
}

func theFieldShouldBeRemovedFromTheTable(field, table string) error {
	if errs != nil {
		return errs
	}
	apiProjection, err := API.GetProjection("Default")
	if err != nil {
		return fmt.Errorf("unexpected error getting projection: %s", err)
	}
	apiProjection1 := apiProjection.(*projections.GORMDB)
	gormDB := apiProjection1.DB()
	if !gormDB.Migrator().HasTable(table) {
		return fmt.Errorf("expected there to be a table %s", table)
	}
	columns, err := gormDB.Migrator().ColumnTypes(table)
	if err != nil {
		return err
	}

	for _, c := range columns {
		if strings.EqualFold(c.Name(), field) {
			return fmt.Errorf("there should be no column %s", field)
		}
	}
	return nil
}

func theServiceIsReset() error {
	tapi, err := api.New(openAPI)
	if err != nil {
		return err
	}
	API = *tapi
	e = API.EchoInstance()
	buf = bytes.Buffer{}
	e.Logger.SetOutput(&buf)
	errs = API.Initialize(scenarioContext)
	return nil
}

func aBlogShouldBeReturnedWithoutField(field string) error {
	if len(responseBody) == 0 {
		err := json.NewDecoder(rec.Body).Decode(&responseBody)
		if err != nil {
			return err
		}
	}

	if _, ok := responseBody[field]; ok {
		return fmt.Errorf("expected to not find field %s", field)
	}
	return nil
}

func isOnTheDeleteScreenWithEntityIdForBlogWithId(arg1, contentType, entityID, id string) error {
	requests[strings.ToLower(contentType+"_delete")] = map[string]interface{}{}
	currScreen = strings.ToLower(contentType + "_delete")
	requests[currScreen]["id"] = id
	requests[currScreen]["entityID"] = entityID
	return nil
}

func isOnTheDeleteScreenWithId(arg1, contentType, id string) error {
	requests[strings.ToLower(contentType+"_delete")] = map[string]interface{}{}
	currScreen = strings.ToLower(contentType + "_delete")
	requests[currScreen]["id"] = id
	return nil
}

func theShouldBeDeleted(contentEntity string, id int) error {
	output := map[string]interface{}{}

	apiProjection, err := API.GetProjection("Default")
	if err != nil {
		return fmt.Errorf("unexpected error getting projection: %s", err)
	}
	apiProjection1 := apiProjection.(*projections.GORMDB)
	searchResult := apiProjection1.DB().Table(strings.Title(contentEntity)).Find(&output, "id = ?", id)
	if len(output) != 0 {
		return fmt.Errorf("the entity was not deleted")
	}
	if searchResult.Error != nil {
		return fmt.Errorf("got error from db query: %s", err)
	}
	return nil
}

func aFilterOnTheFieldEqWithValue(field, value string) error {

	filters = "_filters[" + field + "][eq]=" + value
	return nil
}

func aFilterOnTheFieldEqWithValues(field string, values *godog.Table) error {
	filters = "_filters[" + field + "][eq]="
	for i := 1; i < len(values.Rows); i++ {
		for _, cell := range values.Rows[i].Cells {
			if i == len(values.Rows)-1 {
				filters += cell.Value
			} else {
				filters += cell.Value + ","
			}
		}
	}

	return nil
}

func aFilterOnTheFieldGtWithValue(field, value string) error {
	filters = "_filters[" + field + "][gt]=" + value
	return nil
}

func aFilterOnTheFieldInWithValues(field string, values *godog.Table) error {
	filters = "_filters[" + field + "][in]="
	for i := 1; i < len(values.Rows); i++ {
		for _, cell := range values.Rows[i].Cells {
			if i == len(values.Rows)-1 {
				filters += cell.Value
			} else {
				filters += cell.Value + ","
			}
		}
	}

	return nil
}

func aFilterOnTheFieldLikeWithValue(field, value string) error {
	filters = "_filters[" + field + "][like]=" + value
	return nil
}

func aFilterOnTheFieldLtWithValue(field, value string) error {
	filters = "_filters[" + field + "][lt]=" + value
	return nil
}

func aFilterOnTheFieldNeWithValue(field, value string) error {
	filters = "_filters[" + field + "][ne]=" + value
	return nil
}

func callsTheReplayMethodOnTheEventRepository(arg1 string) error {
	repo, err := API.GetEventStore("Default")
	if err != nil {
		return fmt.Errorf("error getting event store: %s", err)
	}
	eventRepo := repo.(*model.EventRepositoryGorm)
	projection, err := API.GetProjection("Default")
	if err != nil {
		return fmt.Errorf("error getting event store: %s", err)
	}

	factories := API.GetEntityFactories()
	total, success, failed, errArray = eventRepo.ReplayEvents(context.Background(), time.Time{}, factories, projection)
	if err != nil {
		return fmt.Errorf("error getting event store: %s", err)
	}
	return nil
}

func sojournerDeletesTheTable(tableName string) error {
	//output := map[string]interface{}{}

	apiProjection, err := API.GetProjection("Default")
	if err != nil {
		return fmt.Errorf("unexpected error getting projection: %s", err)
	}
	apiProjection1 := apiProjection.(*projections.GORMDB)
	if *driver == "mysql" {
		tables := []string{}
		r := apiProjection1.DB().Debug().Raw(fmt.Sprintf("SELECT TABLE_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE REFERENCED_TABLE_NAME = '%s';", strings.Title(tableName))).Scan(&tables)
		if r.Error != nil {
			return r.Error
		}
		schema := API.Schemas
		for _, t := range tables {
			s := schema[t]
			f := s.GetField("Table")
			f.SetTag(`json:"table_alias" gorm:"default:` + t + `"`)
			instance := s.Build().New()
			json.Unmarshal([]byte(`{
				"table_alias": "`+t+`"
			}`), &instance)
			r := apiProjection1.DB().Debug().Migrator().DropConstraint(instance, tableName)
			if r != nil {
				return r
			}

		}
	}
	result := apiProjection1.DB().Migrator().DropTable(strings.Title(tableName))
	if result != nil {
		return fmt.Errorf("error dropping table: %s got err: %s", tableName, result)
	}

	return nil
}

func theTableShouldBePopulatedWith(contentType string, details *godog.Table) error {
	contentEntity := map[string]interface{}{}
	var result *gorm.DB

	head := details.Rows[0].Cells
	compare := map[string]interface{}{}

	for i := 1; i < len(details.Rows); i++ {
		for n, cell := range details.Rows[i].Cells {
			compare[head[n].Value] = cell.Value
		}

		apiProjection, err := API.GetProjection("Default")
		if err != nil {
			return fmt.Errorf("unexpected error getting projection: %s", err)
		}
		apiProjection1 := apiProjection.(*projections.GORMDB)
		result = apiProjection1.DB().Table(strings.Title(contentType)).Find(&contentEntity, "weos_ID = ?", compare["weos_id"])

		if contentEntity == nil {
			return fmt.Errorf("unexpected error finding content type in db")
		}

		if result.Error != nil {
			return fmt.Errorf("unexpected error finding content type: %s", result.Error)
		}

		for key, value := range compare {
			if key == "sequence_no" {
				strSeq := strconv.Itoa(int(contentEntity[key].(int64)))
				if strSeq != value {
					return fmt.Errorf("expected %s %s %s, got %s", contentType, key, value, contentEntity[key])
				}
			} else {
				if contentEntity[key] != value {
					return fmt.Errorf("expected %s %s %s, got %s", contentType, key, value, contentEntity[key])
				}
			}
		}

	}
	return nil
}

func theTotalNoEventsAndProcessedAndFailuresShouldBeReturned() error {
	if total == 0 && success == 0 && failed == 0 {
		return fmt.Errorf("expected total, success and failed to be non 0 values")
	}
	return nil
}

func aWarningShouldBeShown() error {
	if !strings.Contains(buf.String(), "invalid open id connect url:") {
		return fmt.Errorf("expected an error to be log got '%s'", buf.String())
	}
	return nil
}

func anErrorShouldBeReturned1(statusCode int) error {
	if rec.Code != statusCode {
		return fmt.Errorf("expected response status code to be %d got %d", statusCode, rec.Code)
	}
	return nil
}

func authenticatedAndReceivedAJWT(userName string) error {
	token = os.Getenv("OAUTH_TEST_KEY")
	return nil
}

func hasAValidUserAccount(arg1 string) error {
	return nil
}

func sIdIs(userName, userID string) error {
	return nil
}

func theUserIdOnTheEntityEventsShouldBe(userID string) error {
	var events []map[string]interface{}
	apiProjection, err := API.GetProjection("Default")
	if err != nil {
		return fmt.Errorf("unexpected error getting projection: %s", err)
	}
	apiProjection1 := apiProjection.(*projections.GORMDB)
	eventResult := apiProjection1.DB().Table("gorm_events").Find(&events, "type = ?", "create")
	if eventResult.Error != nil {
		return fmt.Errorf("unexpected error finding events: %s", eventResult.Error)
	}
	if events[len(events)-1]["user"] == "" {
		return fmt.Errorf("expected to find user but got nil")
	}
	return nil
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Before(reset)
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		return ctx, dropDB()
	})
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
	ctx.Step(`^the "([^"]*)" header should be "([^"]*)"$`, theHeaderShouldBe)
	ctx.Step(`^a "([^"]*)" entity configuration should be setup$`, aEntityConfigurationShouldBeSetup)
	ctx.Step(`^"([^"]*)" is on the "([^"]*)" edit screen with id "([^"]*)"$`, isOnTheEditScreenWithId)
	ctx.Step(`^the "([^"]*)" is updated$`, theIsUpdated)
	ctx.Step(`^a header "([^"]*)" with value "([^"]*)"$`, aHeaderWithValue)
	ctx.Step(`^a (\d+) response should be returned$`, aResponseShouldBeReturned)
	ctx.Step(`^the "([^"]*)" endpoint "([^"]*)" is hit$`, theEndpointIsHit)
	ctx.Step(`^a blog should be returned$`, aBlogShouldBeReturned)
	ctx.Step(`^Sojourner is updating "([^"]*)" with id "([^"]*)"$`, sojournerIsUpdatingWithId)
	ctx.Step(`^a warning should be output to logs letting the developer know that a parameter for each part of the idenfier must be set$`, aWarningShouldBeOutputToLogsLettingTheDeveloperKnowThatAParameterForEachPartOfTheIdenfierMustBeSet)
	ctx.Step(`^a "([^"]*)" route should be added to the api$`, aRouteShouldBeAddedToTheApi1)
	ctx.Step(`^a (\d+) response should be returned$`, aResponseShouldBeReturned)
	ctx.Step(`^request body$`, requestBody)
	ctx.Step(`^that the "([^"]*)" binary is generated$`, thatTheBinaryIsGenerated)
	ctx.Step(`^the binary is run with the specification$`, theBinaryIsRunWithTheSpecification)
	ctx.Step(`^the "([^"]*)" endpoint "([^"]*)" is hit$`, theEndpointIsHit)
	ctx.Step(`^the service is running$`, theServiceIsRunning)
	ctx.Step(`^is run on the operating system "([^"]*)" as "([^"]*)"$`, isRunOnTheOperatingSystemAs)
	ctx.Step(`^a warning should be output because the endpoint is invalid$`, aWarningShouldBeOutputBecauseTheEndpointIsInvalid)
	ctx.Step(`^a warning should be output to logs$`, aWarningShouldBeOutputToLogs)
	ctx.Step(`^the "([^"]*)" header should be present$`, theHeaderShouldBePresent)
	ctx.Step(`^the list results should be$`, theListResultsShouldBe)
	ctx.Step(`^the page in the result should be (\d+)$`, thePageInTheResultShouldBe)
	ctx.Step(`^the search button is hit$`, theSearchButtonIsHit)
	ctx.Step(`^the total results should be (\d+)$`, theTotalResultsShouldBe)
	ctx.Step(`^a warning should be output to the logs telling the developer the property doesn\'t exist$`, aWarningShouldBeOutputToTheLogsTellingTheDeveloperThePropertyDoesntExist)
	ctx.Step(`^"([^"]*)" adds the "([^"]*)" attribute to the "([^"]*)" field on the "([^"]*)" content type$`, addsTheAttributeToTheFieldOnTheContentType)
	ctx.Step(`^"([^"]*)" adds the field "([^"]*)" type "([^"]*)" to the "([^"]*)" content type$`, addsTheFieldToTheContentType)
	ctx.Step(`^an error should show letting the developer know that is part of a foreign key reference$`, anErrorShouldShowLettingTheDeveloperKnowThatIsPartOfAForeignKeyReference)
	ctx.Step(`^"([^"]*)" removed the "([^"]*)" field from the "([^"]*)" content type$`, removedTheFieldFromTheContentType)
	ctx.Step(`^the "([^"]*)" field should be removed from the "([^"]*)" table$`, theFieldShouldBeRemovedFromTheTable)
	ctx.Step(`^a blog should be returned without field "([^"]*)"$`, aBlogShouldBeReturnedWithoutField)
	ctx.Step(`^the service is reset$`, theServiceIsReset)
	ctx.Step(`^the "([^"]*)" form is submitted with content type "([^"]*)"$`, theFormIsSubmittedWithContentType)
	ctx.Step(`^the "([^"]*)" is submitted without content type$`, theIsSubmittedWithoutContentType)
	ctx.Step(`^"([^"]*)" is on the "([^"]*)" list screen$`, isOnTheListScreen)
	ctx.Step(`^the items per page are (\d+)$`, theItemsPerPageAre)
	ctx.Step(`^the page no\. is (\d+)$`, thePageNoIs)
	ctx.Step(`^"([^"]*)" is on the "([^"]*)" delete screen with entity id "([^"]*)" for blog with id "([^"]*)"$`, isOnTheDeleteScreenWithEntityIdForBlogWithId)
	ctx.Step(`^"([^"]*)" is on the "([^"]*)" delete screen with id "([^"]*)"$`, isOnTheDeleteScreenWithId)
	ctx.Step(`^the "([^"]*)" "(\d+)" should be deleted$`, theShouldBeDeleted)
	ctx.Step(`^a filter on the field "([^"]*)" "eq" with value "([^"]*)"$`, aFilterOnTheFieldEqWithValue)
	ctx.Step(`^a filter on the field "([^"]*)" "eq" with values$`, aFilterOnTheFieldEqWithValues)
	ctx.Step(`^a filter on the field "([^"]*)" "gt" with value "([^"]*)"$`, aFilterOnTheFieldGtWithValue)
	ctx.Step(`^a filter on the field "([^"]*)" "in" with values$`, aFilterOnTheFieldInWithValues)
	ctx.Step(`^a filter on the field "([^"]*)" "like" with value "([^"]*)"$`, aFilterOnTheFieldLikeWithValue)
	ctx.Step(`^a filter on the field "([^"]*)" "lt" with value "([^"]*)"$`, aFilterOnTheFieldLtWithValue)
	ctx.Step(`^a filter on the field "([^"]*)" "ne" with value "([^"]*)"$`, aFilterOnTheFieldNeWithValue)
	ctx.Step(`^"([^"]*)" calls the replay method on the event repository$`, callsTheReplayMethodOnTheEventRepository)
	ctx.Step(`^Sojourner" deletes the "([^"]*)" table$`, sojournerDeletesTheTable)
	ctx.Step(`^the "([^"]*)" table should be populated with$`, theTableShouldBePopulatedWith)
	ctx.Step(`^the total no\. events and processed and failures should be returned$`, theTotalNoEventsAndProcessedAndFailuresShouldBeReturned)
	ctx.Step(`^a warning should be shown$`, aWarningShouldBeShown)
	ctx.Step(`^an (\d+) error should be returned$`, anErrorShouldBeReturned1)
	ctx.Step(`^"([^"]*)" authenticated and received a JWT$`, authenticatedAndReceivedAJWT)
	ctx.Step(`^"([^"]*)" has a valid user account$`, hasAValidUserAccount)
	ctx.Step(`^"([^"]*)"\'s id is "([^"]*)"$`, sIdIs)
	ctx.Step(`^the user id on the entity events should be "([^"]*)"$`, theUserIdOnTheEntityEventsShouldBe)
}

func TestBDD(t *testing.T) {
	status := godog.TestSuite{
		Name:                 "BDD Tests",
		ScenarioInitializer:  InitializeScenario,
		TestSuiteInitializer: InitializeSuite,
		Options: &godog.Options{
			Format: "pretty",
			Tags:   "~long && ~skipped",
			//Tags: "focus",
			//Tags: "WEOS-1110 && ~skipped",
		},
	}.Run()
	if status != 0 {
		t.Errorf("there was an error running tests, exit code %d", status)
	}
}
