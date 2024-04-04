package rest_test

import (
	"github.com/wepala/weos/v2/rest"
	"os"
	"testing"
)

func TestConfig(t *testing.T) {
	os.Setenv("WEOS_SPEC", "./fixtures/api_with_environment_variables.yaml")
	os.Setenv("DB_DRIVER", "sqlite3")
	os.Setenv("BASE_PATH", "/local")
	spec, err := rest.Config()
	if err != nil {
		t.Fatal(err)
	}
	if spec == nil {
		t.Fatal("spec is nil")
	}
	if data, ok := spec.Extensions[rest.WeOSConfigExtension]; ok {
		if basePath, ok := data.(map[string]interface{})["basePath"]; ok {
			if basePath != "/local" {
				t.Errorf("expected basePath to be /local but got %s", basePath)
			}
		}
	}

}
