package rest_test

import (
	"io/ioutil"
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
			if table.Builder.GetField("Table") == nil {
				t.Fatalf("expected a table field")
			}
		}

		//check for foreign key on Post table to Author
		postTable, ok := result["Post"]
		if !ok {
			t.Fatalf("expected to find a table Post")
		}

		if !postTable.Builder.HasField("AuthorEmail") {
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
			if table.Builder.GetField("Table") == nil {
				t.Fatalf("expected a table field")
			}
		}

		//check keys on table Author
		authorTable, ok := result["Author"]
		if !ok {
			t.Fatalf("expected to find a table Author")
		}

		if !authorTable.Builder.HasField("Id") {
			t.Errorf("expected the struct to have field '%s'", "Id")
		}

		if authorTable.Builder.HasField("Email") {
			t.Errorf("expected the struct to not have field '%s'", "Email")
		}
	})
}
