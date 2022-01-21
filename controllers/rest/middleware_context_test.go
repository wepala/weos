package rest_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos-service/context"
	"github.com/wepala/weos-service/controllers/rest"
)

func TestContext(t *testing.T) {
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
	t.Run("check that account id is added by default", func(t *testing.T) {
		accountID := "123"
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(nil, swagger, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			tAccountID := context.GetAccount(cc)
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
		mw := rest.Context(nil, swagger, path, path.Post)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(paramName) == nil {
				t.Fatalf("expected a value to be returned for '%s'", paramName)
			}
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

	t.Run("context name should match "+rest.ContextNameExtension, func(t *testing.T) {
		contextName := "soh"
		paramName := "someOtherHeader"
		paramValue := "123"
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(nil, swagger, path, path.Post)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(contextName) == nil {
				t.Fatalf("expected a value to be returned for '%s'", contextName)
			}
			tValue := cc.Value(contextName).(string)
			if tValue != paramValue {
				t.Errorf("expected the param '%s' to have value '%s', got '%v'", contextName, paramValue, tValue)
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

	t.Run("parameter in query string should be added to context", func(t *testing.T) {
		paramName := "q"
		paramValue := "123"
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(nil, swagger, path, path.Post)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(paramName) == nil {
				t.Fatalf("expected a value to be returned for '%s'", paramName)
			}
			tValue := cc.Value(paramName).(string)
			if tValue != paramValue {
				t.Errorf("expected the param '%s' to have value '%s', got '%v'", paramName, paramValue, tValue)
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs?"+paramName+"="+paramValue, nil)
		e.POST("/blogs", handler)
		e.ServeHTTP(resp, req)
	})

	t.Run("parameter in the path string should be added to context", func(t *testing.T) {
		paramName := "id"
		paramValue := "123"
		path := swagger.Paths.Find("/blogs/:id")
		if path == nil {
			t.Fatal("could not find expected path")
		}
		mw := rest.Context(nil, swagger, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(paramName) == nil {
				t.Fatalf("expected a value to be returned for '%s'", paramName)
			}
			tValue := cc.Value(paramName).(string)
			if tValue != paramValue {
				t.Errorf("expected the param '%s' to have value '%s', got '%v'", paramName, paramValue, tValue)
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs/"+paramValue, nil)
		e.GET("/blogs/:"+paramName, handler)
		e.ServeHTTP(resp, req)
	})

	t.Run("parameter in the query string  of number type should be added to context", func(t *testing.T) {
		paramName := "cost"
		paramValue := 123
		path := swagger.Paths.Find("/blogs/:id")
		if path == nil {
			t.Fatal("could not find expected path")
		}
		mw := rest.Context(nil, swagger, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(paramName) == nil {
				t.Fatalf("expected a value to be returned for '%s'", paramName)
			}
			tValue := cc.Value(paramName).(int)
			if tValue != paramValue {
				t.Errorf("expected the param '%s' to have value '%d', got '%v'", paramName, paramValue, tValue)
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs/?"+fmt.Sprint(paramValue), nil)
		e.GET("/blogs/:1?"+paramName, handler)
		e.ServeHTTP(resp, req)
	})

	t.Run("parameter in the query string  of number type with format type should be added to context", func(t *testing.T) {
		paramName := "leverage"
		paramValue := 123.00
		path := swagger.Paths.Find("/blogs/:id")
		if path == nil {
			t.Fatal("could not find expected path")
		}
		mw := rest.Context(nil, swagger, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(paramName) == nil {
				t.Fatalf("expected a value to be returned for '%s'", paramName)
			}
			tValue := cc.Value(paramName).(float64)
			if tValue != paramValue {
				t.Errorf("expected the param '%s' to have value '%f', got '%v'", paramName, paramValue, tValue)
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs/?"+fmt.Sprint(paramValue), nil)
		e.GET("/blogs/:1?"+paramName, handler)
		e.ServeHTTP(resp, req)
	})

	t.Run("undefined parameter should NOT be added to context", func(t *testing.T) {
		paramName := "Asdfsdgfsdfgypypadfasd"
		paramValue := "123"
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(nil, swagger, path, path.Post)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(paramName) != nil {
				t.Errorf("did not expect to get a value, got '%s'", cc.Value(paramName).(string))
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

	t.Run("if no middleware is defined it should work", func(t *testing.T) {
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(nil, swagger, path, path.Post)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", nil)
		e.POST("/blogs", handler)
		e.ServeHTTP(resp, req)
	})
}
