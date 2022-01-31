package rest_test

import (
	"bytes"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/controllers/rest"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestStandardMiddlewares_CSVUpload(t *testing.T) {
	//setup OpenAPI spec
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

	t.Run("import basic blogs", func(t *testing.T) {
		path := swagger.Paths.Find("/blogs")
		contextHasItems := false

		csvUploadMiddleware := rest.CSVUpload(nil, swagger, path, path.Post)
		controller := func(ctxt echo.Context) error {
			var csvItems [][]string
			//confirm that the items are in the context
			items := ctxt.Request().Context().Value("_items")
			//if items is an array let's proceed
			if csvItems, contextHasItems = items.([][]string); contextHasItems {
				//do more checks
				if len(csvItems) != 2 {
					t.Fatalf("expected %d items, got %d", 2, len(csvItems))
				}
				if csvItems[1][0] != "some title" {
					t.Errorf("expected row %d column %d to be '%s', got '%s'", 1, 0, "some title", csvItems[1][0])
				}

			}
			return nil
		}
		e := echo.New()
		resp := httptest.NewRecorder()
		body, err := ioutil.ReadFile("./fixtures/blogs.csv")
		if err != nil {
			t.Fatalf("error reading csv fixture '%s'", err)
		}
		req := httptest.NewRequest(http.MethodPut, "/blogs", bytes.NewReader(body))
		req.Header.Set("content-type", "text/csv")
		e.PUT("/blogs", controller, csvUploadMiddleware)
		e.ServeHTTP(resp, req)

		if !contextHasItems {
			t.Errorf("expected context to have _items")
		}
	})

	t.Run("import csv if the content type is text/csv", func(t *testing.T) {

	})

	t.Run("import csv if one field in multipart form is named or aliased _csv_upload", func(t *testing.T) {

	})
}
