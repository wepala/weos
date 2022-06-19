package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"testing"
)

func LoadConfig(t *testing.T, file string) (*openapi3.Swagger, error) {
	//load config
	content, err := ioutil.ReadFile(file)
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
	return loader.LoadSwaggerFromData(content)
}
