package rest_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"golang.org/x/net/context"

	api "github.com/wepala/weos/controllers/rest"
)

func TestRESTAPI_Initialize_Basic(t *testing.T) {
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
	t.Run("basic schema", func(t *testing.T) {
		defer os.Remove("test.db")
		openApi := `openapi: 3.0.3
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
    Category:
      type: object
      properties:
        title:
          type: string
        description:
          type: string
      required:
        - title
      x-identifier:
        - title
`
		tapi, err := api.New(openApi)
		if err != nil {
			t.Errorf("unexpected error: '%s'", err)
		}
		err = tapi.Initialize(context.TODO())
		if err != nil {
			t.Fatalf("unexpected error initializing api '%s'", err)
		}
		//check that the table was created on the default projection
		var defaultProjection model.Projection
		if defaultProjection, err = tapi.GetProjection("Default"); err != nil {
			t.Fatalf("unexpected error getting default projection '%s'", err)
		}
		var ok bool
		var defaultGormProject *projections.GORMDB
		if defaultGormProject, ok = defaultProjection.(*projections.GORMDB); !ok {
			t.Fatalf("unexpected error getting default projection '%s'", err)
		}

		if !defaultGormProject.DB().Migrator().HasTable("Category") {
			t.Errorf("expected categories table to exist")
		}
	})
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}

func TestRESTAPI_Initialize_CreateAddedToPost(t *testing.T) {
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
	tapi, err := api.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	err = tapi.Initialize(context.TODO())
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	e := tapi.EchoInstance()
	mockBlog := &Blog{Title: "Test Blog", Url: "www.testBlog.com"}
	reqBytes, err := json.Marshal(mockBlog)
	if err != nil {
		t.Fatalf("error setting up request %s", err)
	}
	body := bytes.NewReader(reqBytes)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/blogs", body)
	e.ServeHTTP(resp, req)
	//confirm that the response is not 404
	if resp.Result().StatusCode == http.StatusNotFound {
		t.Errorf("expected the response code to not be %d, got %d", http.StatusNotFound, resp.Result().StatusCode)
	}
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}

func TestRESTAPI_Initialize_CreateBatchAddedToPost(t *testing.T) {
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
	tapi, err := api.New("./fixtures/blog-create-batch.yaml")
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	err = tapi.Initialize(nil)
	if err != nil {
		t.Fatalf("unexpected error loading spec '%s'", err)
	}
	e := tapi.EchoInstance()
	mockBlog := &[3]Blog{
		{ID: "1asdas3", Title: "Blog 1", Url: "www.testBlog1.com"},
		{ID: "2gf233", Title: "Blog 2", Url: "www.testBlog2.com"},
		{ID: "3dgff3", Title: "Blog 3", Url: "www.testBlog3.com"},
	}
	reqBytes, err := json.Marshal(mockBlog)
	if err != nil {
		t.Fatalf("error setting up request %s", err)
	}
	body := bytes.NewReader(reqBytes)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/blogs", body)
	e.ServeHTTP(resp, req)
	//confirm that the response is not 404
	if resp.Result().StatusCode == http.StatusNotFound {
		t.Errorf("expected the response code to be %d, got %d", http.StatusNotFound, resp.Result().StatusCode)
	}
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}

func TestRESTAPI_Initialize_HealthCheck(t *testing.T) {
	//make sure healthcheck is being added
	os.Remove("test.db")
	tapi, err := api.New("./fixtures/blog-create-batch.yaml")
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	err = tapi.Initialize(context.TODO())
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	e := tapi.EchoInstance()

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	e.ServeHTTP(resp, req)
	//confirm that the response is 200
	if resp.Result().StatusCode != http.StatusOK {
		t.Errorf("expected the response code to be %d, got %d", http.StatusOK, resp.Result().StatusCode)
	}
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}

type TestBlog struct {
	ID          *string `json:"id"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Url         *string `json:"url"`
}

func TestRESTAPI_Initialize_RequiredField(t *testing.T) {
	tapi, err := api.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	err = tapi.Initialize(context.TODO())
	if err != nil {
		t.Fatalf("un expected error initializing api '%s'", err)
	}
	e := tapi.EchoInstance()
	t.Run("sending blog without a title and url which are required fields", func(t *testing.T) {
		description := "testing 1st blog description"
		mockBlog := &TestBlog{Description: &description}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error setting up request %s", err)
		}
		body := bytes.NewReader(reqBytes)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", "application/json")
		e.ServeHTTP(resp, req)
		if resp.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("expected the response code to be %d, got %d", http.StatusBadRequest, resp.Result().StatusCode)
		}
	})
	t.Run("sending blog without a description which is not a required field", func(t *testing.T) {
		title := "blog title"
		url := "ww.blogtest.com"
		mockBlog := &TestBlog{Title: &title, Url: &url}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error setting up request %s", err)
		}
		body := bytes.NewReader(reqBytes)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", "application/json")
		e.ServeHTTP(resp, req)
		if resp.Result().StatusCode != http.StatusCreated {
			t.Errorf("expected the response code to be %d, got %d", http.StatusCreated, resp.Result().StatusCode)
		}
	})
}

func TestRESTAPI_Initialize_UpdateAddedToPut(t *testing.T) {
	os.Remove("test.db")
	tapi, err := api.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	err = tapi.Initialize(nil)
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	e := tapi.EchoInstance()
	found := false
	method := "PUT"
	path := "/blogs/:id"
	middleware := "Update"
	routes := e.Routes()
	for _, route := range routes {
		if route.Method == method && route.Path == path && strings.Contains(route.Name, middleware) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find update path")
	}

}

func TestRESTAPI_Initialize_UpdateAddedToPatch(t *testing.T) {
	os.Remove("test.db")
	tapi, err := api.New("./fixtures/blog-create-batch.yaml")
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	err = tapi.Initialize(nil)
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	e := tapi.EchoInstance()
	found := false
	method := "PATCH"
	path := "/blogs/:id"
	middleware := "Update"
	routes := e.Routes()
	for _, route := range routes {
		if route.Method == method && route.Path == path && strings.Contains(route.Name, middleware) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find update path")
	}

}

func TestRESTAPI_Initialize_ViewAddedToGet(t *testing.T) {
	os.Remove("test.db")
	tapi, err := api.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	err = tapi.Initialize(nil)
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	e := tapi.EchoInstance()

	found := false
	method := "GET"
	path := "/blogs/:id"
	middleware := "ViewController"
	routes := e.Routes()
	for _, route := range routes {
		if route.Method == method && route.Path == path && strings.Contains(route.Name, middleware) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find update path")
	}
}

func TestRESTAPI_Initialize_ListAddedToGet(t *testing.T) {
	os.Remove("test.db")
	tapi, err := api.New("./fixtures/blog-create-batch.yaml")
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	err = tapi.Initialize(nil)
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	e := tapi.EchoInstance()
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/blogs", nil)
	e.ServeHTTP(resp, req)
	//confirm that the response is not 404
	if resp.Result().StatusCode == http.StatusNotFound {
		t.Errorf("expected the response code to be %d, got %d", http.StatusNotFound, resp.Result().StatusCode)
	}
	os.Remove("test.db")
}

func TestRESTAPI_RegisterCommandDispatcher(t *testing.T) {
	tapi := &api.RESTAPI{}
	tapi.RegisterCommandDispatcher("test", &model.DefaultCommandDispatcher{})
	//get dispatcher
	_, err := tapi.GetCommandDispatcher("test")
	if err != nil {
		t.Fatalf("unexpected error getting dispatcher '%s'", err)
	}
}

func TestRESTAPI_RegisterEventDispatcher(t *testing.T) {
	tapi := &api.RESTAPI{}
	tapi.RegisterEventStore("test", &model.EventRepositoryGorm{})
	//get dispatcher
	_, err := tapi.GetEventStore("test")
	if err != nil {
		t.Fatalf("unexpected error getting dispatcher '%s'", err)
	}
}

func TestRESTAPI_RegisterProjection(t *testing.T) {
	tapi := &api.RESTAPI{}
	tapi.RegisterProjection("test", &projections.GORMDB{})
	//get dispatcher
	_, err := tapi.GetProjection("test")
	if err != nil {
		t.Fatalf("unexpected error getting projection '%s'", err)
	}
}

func TestRESTAPI_DefaultProjectionRegisteredBefore(t *testing.T) {
	os.Remove("test.db")
	tapi, err := api.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	_, gormDB, err := tapi.SQLConnectionFromConfig(tapi.Config.Database)
	gormProjection, err := projections.NewProjection(context.TODO(), gormDB, tapi.EchoInstance().Logger)
	if err != nil {
		t.Fatalf("error setting up gorm projection")
	}
	//setup mock projection as anonymous struct
	mockProjection := &struct {
		*projections.GORMDB
		Test bool
	}{
		gormProjection,
		true,
	}
	tapi.RegisterProjection("Default", mockProjection)
	err = tapi.Initialize(context.TODO())
	if err != nil {
		t.Fatalf("unexpected error '%s'", err)
	}
	//get the projection from the api
	defaultProjection, err := tapi.GetProjection("Default")
	if err != nil {
		t.Fatalf("unexpected error getting default projection '%s'", err)
	}
	if _, ok := defaultProjection.(*struct {
		*projections.GORMDB
		Test bool
	}); !ok {
		t.Errorf("expected the projection to be the one that was set")
	}
}

func TestRESTAPI_Initialize_DiscoveryAddedToGet(t *testing.T) {
	os.Remove("test.db")
	tapi, err := api.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	err = tapi.Initialize(nil)
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	e := tapi.EchoInstance()

	found := false
	method := "GET"
	path := "/api"
	middleware := "APIDiscovery"
	routes := e.Routes()
	for _, route := range routes {
		if route.Method == method && route.Path == path && strings.Contains(route.Name, middleware) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find get path")
	}
}

func TestRESTAPI_Initialize_DefaultResponseMiddlware(t *testing.T) {
	//make sure Default middleware is added
	os.Remove("test.db")
	tapi, err := api.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading spec '%s'", err)
	}
	err = tapi.Initialize(context.TODO())
	if err != nil {
		t.Fatalf("unexpected error loading spec '%s'", err)
	}
	e := tapi.EchoInstance()

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	e.ServeHTTP(resp, req)
	//confirm that the response is not 404
	if resp.Result().StatusCode == http.StatusNotFound {
		t.Errorf("expected the response code to not be %d, got %d", http.StatusNotFound, resp.Result().StatusCode)
	}
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}

func TestRESTAPI_Static(t *testing.T) {
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
	t.Run("basic schema", func(t *testing.T) {
		defer os.Remove("test.db")
		openApi := `openapi: 3.0.3
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
    Category:
      type: object
      properties:
        title:
          type: string
        description:
          type: string
      required:
        - title
      x-identifier:
        - title
paths:
  /:
    get:
      responses:
        200:
          description: file found
          x-folder: "./fixtures/staticF"
        404:
          description: file not found
`
		tapi, err := api.New(openApi)
		if err != nil {
			t.Errorf("unexpected error: '%s'", err)
		}
		err = tapi.Initialize(context.TODO())
		if err != nil {
			t.Fatalf("unexpected error initializing api '%s'", err)
		}
		//check that the table was created on the default projection
		var defaultProjection model.Projection
		if defaultProjection, err = tapi.GetProjection("Default"); err != nil {
			t.Fatalf("unexpected error getting default projection '%s'", err)
		}
		var ok bool
		var defaultGormProject *projections.GORMDB
		if defaultGormProject, ok = defaultProjection.(*projections.GORMDB); !ok {
			t.Fatalf("unexpected error getting default projection '%s'", err)
		}

		if !defaultGormProject.DB().Migrator().HasTable("Category") {
			t.Errorf("expected categories table to exist")
		}

		found := false
		yamlRoutes := tapi.EchoInstance().Routes()
		for _, route := range yamlRoutes {
			if strings.Contains(route.Name, "static") {
				found = true
			}
		}
		if found == false {
			t.Errorf("expected the static folder to be present on routes")
		}
	})
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}

func TestRESTAPI_File(t *testing.T) {
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
	t.Run("basic schema", func(t *testing.T) {
		defer os.Remove("test.db")
		openApi := `openapi: 3.0.3
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
    Category:
      type: object
      properties:
        title:
          type: string
        description:
          type: string
      required:
        - title
      x-identifier:
        - title
paths:
  /file:
    get:
      responses:
        200:
          description: file found
          x-file: "./fixtures/staticF/index"
        404:
          description: file not found
`
		tapi, err := api.New(openApi)
		if err != nil {
			t.Errorf("unexpected error: '%s'", err)
		}
		err = tapi.Initialize(context.TODO())
		if err != nil {
			t.Fatalf("unexpected error initializing api '%s'", err)
		}
		//check that the table was created on the default projection
		var defaultProjection model.Projection
		if defaultProjection, err = tapi.GetProjection("Default"); err != nil {
			t.Fatalf("unexpected error getting default projection '%s'", err)
		}
		var ok bool
		var defaultGormProject *projections.GORMDB
		if defaultGormProject, ok = defaultProjection.(*projections.GORMDB); !ok {
			t.Fatalf("unexpected error getting default projection '%s'", err)
		}

		if !defaultGormProject.DB().Migrator().HasTable("Category") {
			t.Errorf("expected categories table to exist")
		}

		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/file", nil)
		tapi.EchoInstance().ServeHTTP(resp, req)

		respBody := resp.Body
		if respBody.String() != "<html><head><title>Test Page</title></head><body>Test Page</body></html>" {
			t.Errorf("expected the response to be: %s, got %s", "<html><head><title>Test Page</title></head><body>Test Page</body></html>", respBody.String())
		}
	})
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}

func TestRESTAPI_InitializeSecurity(t *testing.T) {
	//This test is to show that a schema can be defined but the global “security” don't need to be specified
	t.Run("basic security schema specified without global security specified", func(t *testing.T) {
		openApi := `openapi: 3.0.3
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
  securitySchemes:
    Auth0:
      type: openIdConnect
      openIdConnectUrl: https://dev-bhjqt6zc.us.auth0.com/.well-known/openid-configuration
  schemas:
    Category:
      type: object
      properties:
        title:
          type: string
        description:
          type: string
      required:
        - title
      x-identifier:
        - title
`
		tapi, err := api.New(openApi)
		if err != nil {
			t.Errorf("unexpected error: '%s'", err)
		}
		err = tapi.Initialize(context.TODO())
		if err != nil {
			t.Fatalf("unexpected error initializing api '%s'", err)
		}

	})
	os.Remove("test.db")
}
