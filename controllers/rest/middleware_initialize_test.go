package rest_test

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/controllers/rest"
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
		for _, table := range result {
			if table.GetField("Table") == nil {
				t.Fatalf("expected a table field")
			}
		}

		//check for foreign key on Post table to Author
		postTable, ok := result["Post"]
		if !ok {
			t.Fatalf("expected to find a table Post")
		}

		if !postTable.HasField("AuthorEmail") {
			t.Errorf("expected the struct to have field '%s'", "AuthorEmail")
		}
	})
}

func TestXRemove(t *testing.T) {
	t.Run("table name is set correctly", func(t *testing.T) {
		content, err := ioutil.ReadFile("./fixtures/blog-delete-content-field.yaml")
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
		for _, table := range result {
			if table.GetField("Table") == nil {
				t.Fatalf("expected a table field")
			}
		}

		//check keys on table Author
		authorTable, ok := result["Author"]
		if !ok {
			t.Fatalf("expected to find a table Author")
		}

		if !authorTable.HasField("Id") {
			t.Errorf("expected the struct to have field '%s'", "Id")
		}

		if authorTable.HasField("Email") {
			t.Errorf("expected the struct to not have field '%s'", "Email")
		}
	})
}

func TestAuthenticateMiddleware(t *testing.T) {
	//instantiate api
	api, err := rest.New("./fixtures/blog-security.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	err = api.Initialize(context.TODO())
	if err != nil {
		t.Fatalf("un expected error initializing api '%s'", err)
	}
	e := api.EchoInstance()

	t.Run("no jwt token added when required", func(t *testing.T) {
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
		if resp.Result().StatusCode != http.StatusUnauthorized {
			t.Errorf("expected the response code to be %d, got %d", http.StatusUnauthorized, resp.Result().StatusCode)
		}
	})
	t.Run("security parameter array is empty", func(t *testing.T) {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		e.ServeHTTP(resp, req)
		if resp.Result().StatusCode != http.StatusOK {
			t.Errorf("expected the response code to be %d, got %d", http.StatusOK, resp.Result().StatusCode)
		}
	})
	t.Run("jwt token added", func(t *testing.T) {
		description := "testing 1st blog description"
		mockBlog := &TestBlog{Description: &description}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error setting up request %s", err)
		}
		token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJKb2huIiwibmFtZSI6IkpvaG4gRG9lIiwiZW1haWwiOiJqRG9lQGdtYWlsLmNvbSIsImlhdCI6MTY0NDg4Nzc2OSwiZXhwIjoxNjQ0ODg4NzY5fQ.sBL03kXCIbjzD5MdzCRb71g8LLQhgr9R7a0-3cJxySw"
		body := bytes.NewReader(reqBytes)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		e.ServeHTTP(resp, req)
		if resp.Result().StatusCode != http.StatusCreated {
			t.Errorf("expected the response code to be %d, got %d", http.StatusCreated, resp.Result().StatusCode)
		}
	})
}
