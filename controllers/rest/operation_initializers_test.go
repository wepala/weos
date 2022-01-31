package rest_test

import (
	"github.com/wepala/weos/controllers/rest"
	"golang.org/x/net/context"
	"net/http"
	"testing"
)

func TestEntityFactoryInitializer(t *testing.T) {
	api, err := rest.New("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error loading api '%s'", err)
	}
	t.Run("get schema from request body", func(t *testing.T) {
		ctxt, err := rest.EntityFactoryInitializer(context.TODO(), api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		entityFactory := rest.GetEntityFactory(ctxt)
		if entityFactory == nil {
			t.Fatalf("expected entity factory to be in the context")
		}
		if entityFactory.Name() != "Blog" {
			t.Errorf("expected the factory name to be '%s', got '%s'", "Blog", entityFactory.Name())
		}
	})
	t.Run("get schema from items in request body", func(t *testing.T) {
		api, err = rest.New("./fixtures/blog-create-batch.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		ctxt, err := rest.EntityFactoryInitializer(context.TODO(), api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		entityFactory := rest.GetEntityFactory(ctxt)
		if entityFactory == nil {
			t.Fatalf("expected entity factory to be in the context")
		}
		if entityFactory.Name() != "Blog" {
			t.Errorf("expected the factory name to be '%s', got '%s'", "Blog", entityFactory.Name())
		}
	})
	t.Run("use the x-schema extension to specify schema", func(t *testing.T) {
		api, err = rest.New("./fixtures/blog-pk-guid-title.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		ctxt, err := rest.EntityFactoryInitializer(context.TODO(), api, "/blogs", http.MethodPost, api.Swagger, api.Swagger.Paths["/blogs"], api.Swagger.Paths["/blogs"].Post)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		entityFactory := rest.GetEntityFactory(ctxt)
		if entityFactory == nil {
			t.Fatalf("expected entity factory to be in the context")
		}
		if entityFactory.Name() != "Blog" {
			t.Errorf("expected the factory name to be '%s', got '%s'", "Blog", entityFactory.Name())
		}
	})

}
