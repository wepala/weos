package rest_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	api "github.com/wepala/weos-service/controllers/rest"
)

func TestRESTAPI_Initialize_Basic(t *testing.T) {
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
	t.Run("basic schema", func(t *testing.T) {
		defer os.Remove("test.db")
		e := echo.New()
		tapi := api.RESTAPI{}
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
		api.Initialize(e, &tapi, openApi)
		if !tapi.Application.DB().Migrator().HasTable("category") {
			t.Errorf("expected categories table to exist")
		}
	})
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}

func TestRESTAPI_Initialize_CreateAddedToPost(t *testing.T) {
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
	t.Run("create controller is added to POST endpoints that don't have a controller and is configured correctly", func(t *testing.T) {
		e := echo.New()
		tapi := api.RESTAPI{}
		api.Initialize(e, &tapi, "./fixtures/blog.yaml")
		mockBlog := &Blog{
			Title: "Test Blog",
		}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error setting up request %s", err)
		}
		body := bytes.NewReader(reqBytes)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		e.ServeHTTP(resp, req)
		//confirm that the response is 201
		if resp.Result().StatusCode != http.StatusCreated {
			t.Errorf("expected the response code to be %d, got %d", http.StatusCreated, resp.Result().StatusCode)
		}
	})
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}
