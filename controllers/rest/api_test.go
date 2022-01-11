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
		_, err := api.Initialize(e, &tapi, openApi)
		if err != nil {
			t.Errorf("unexpected error: '%s'", err)
		}
		if !tapi.Application.DB().Migrator().HasTable("Category") {
			t.Errorf("expected categories table to exist")
		}
	})
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}

func TestRESTAPI_Initialize_CreateAddedToPost(t *testing.T) {
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
	e := echo.New()
	tapi := api.RESTAPI{}
	_, err := api.Initialize(e, &tapi, "./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error '%s'", err)
	}
	mockBlog := &Blog{Title: "Test Blog", Url: "www.testBlog.com"}
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
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
}

func TestRESTAPI_Initialize_CreateBatchAddedToPost(t *testing.T) {
	os.Remove("test.db")
	time.Sleep(1 * time.Second)
	e := echo.New()
	tapi := api.RESTAPI{}
	_, err := api.Initialize(e, &tapi, "./fixtures/blog-create-batch.yaml")
	if err != nil {
		t.Fatalf("unexpected error '%s'", err)
	}
	mockBlog := &[3]Blog{
		{ID: "1", Title: "Blog 1", Url: "www.testBlog1.com"},
		{ID: "2", Title: "Blog 2", Url: "www.testBlog2.com"},
		{ID: "3", Title: "Blog 3", Url: "www.testBlog3.com"},
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
	e := echo.New()
	tapi := api.RESTAPI{}
	_, err := api.Initialize(e, &tapi, "./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error '%s'", err)
	}
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
		e.ServeHTTP(resp, req)
		if resp.Result().StatusCode != http.StatusCreated {
			t.Errorf("expected the response code to be %d, got %d", http.StatusCreated, resp.Result().StatusCode)
		}
	})
}
