package rest_test

import (
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	dynamicstruct "github.com/ompluscator/dynamic-struct"
	"github.com/wepala/weos-service/controllers/rest"
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

func TestCreateSchema_RequiredField(t *testing.T) {
	t.Run("Required Field it set to not nullable", func(t *testing.T) {
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

		schemas := swagger.Components.Schemas

		for _, scheme := range schemas {
			for key, value := range scheme.Value.Required {

				for tableName, table := range result {
					reader := dynamicstruct.NewReader(table)
					field := reader.GetField(value)
					if field == nil {
						t.Fatalf("expected a field")
					}
				}
			}
		}

		////loop through and confirm each has a table name set
		//for tableName, table := range result {
		//	reader := dynamicstruct.NewReader(table)
		//	if reader.GetField("Table") == nil {
		//		t.Fatalf("expected a table field")
		//	}
		//	if reader.GetField("Table").String() != tableName {
		//		t.Errorf("There was an error setting the table name, expected '%s'", tableName)
		//	}
		//}
		//
		////check for foreign key on Post table to Author
		//postTable, ok := result["Post"]
		//if !ok {
		//	t.Fatalf("expected to find a table Post")
		//}
		//
		//reader := dynamicstruct.NewReader(postTable)
		//if !reader.HasField("AuthorId") {
		//	t.Errorf("expected the struct to have field '%s'", "AuthorId")
		//}
		//
		//if !reader.HasField("AuthorEmail") {
		//	t.Errorf("expected the struct to have field '%s'", "AuthorEmail")
		//}
	})
}
