package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos-service/context"
	"github.com/wepala/weos-service/controllers/rest"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContext(t *testing.T) {
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromFile("./fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error loading api specification '%s'", err)
	}
	t.Run("check that account id is added by default", func(t *testing.T) {
		accountID := "123"
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(path.Get, path, swagger)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.(*context.Context)
			tAccountID := context.GetAccount(cc.RequestContext())
			if tAccountID != accountID {
				t.Errorf("expected the account id to be '%s', got '%s'", accountID, tAccountID)
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs", nil)
		req.Header.Set(context.HeaderXAccountID, accountID)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
	})

	t.Run("parameter in the header should be added to context", func(t *testing.T) {
		paramName := "someHeader"
		paramValue := "123"
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(path.Post, path, swagger)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.(*context.Context)
			tValue := cc.Value(paramName).(string)
			if tValue != paramValue {
				t.Errorf("expected the param '%s' to have value '%s', got '%v'", paramName, paramValue, tValue)
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", nil)
		req.Header.Set(paramName, paramValue)
		e.POST("/blogs", handler)
		e.ServeHTTP(resp, req)
	})

}
