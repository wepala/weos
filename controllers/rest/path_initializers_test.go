package rest_test

import (
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	"golang.org/x/net/context"
	"net/http"
	"testing"
)

func TestCORsInitializer(t *testing.T) {
	api, err := rest.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	schemas := rest.CreateSchema(context.TODO(), api.EchoInstance(), api.Swagger)
	baseCtxt := context.WithValue(context.TODO(), weoscontext.SCHEMA_BUILDERS, schemas)
	api.RegisterController("CreateController", rest.CreateController)
	api.RegisterController("ListController", rest.ListController)
	api.RegisterController("UpdateController", rest.UpdateController)
	api.RegisterController("ViewController", rest.ViewController)
	baseCtxt = context.WithValue(baseCtxt, weoscontext.METHODS_FOUND, []string{http.MethodPost, http.MethodGet})
	t.Run("CORS route added", func(t *testing.T) {
		_, err = rest.CORsInitializer(baseCtxt, api, "/blogs", api.Swagger, api.Swagger.Paths["/blogs"])
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		routes := api.EchoInstance().Routes()
		foundCorsRoute := false
		for _, route := range routes {
			if route.Path == "/blogs" && route.Method == http.MethodOptions {
				foundCorsRoute = true
				break
			}
		}
		if !foundCorsRoute {
			t.Error("unable to find CORS routes route")
		}
	})
}
