package rest_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
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
	entityFactory := &EntityFactoryMock{
		SchemaFunc: func() *openapi3.Schema {
			return swagger.Components.Schemas["Blog"].Value
		},
	}
	e := echo.New()
	restApi := &rest.RESTAPI{}
	restApi.SetEchoInstance(e)

	t.Run("check that account id is added by default", func(t *testing.T) {
		accountID := "123"
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
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
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Post)
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
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Post)
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
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Post)
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
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
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
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
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
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
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
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Post)
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
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Post)
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
	t.Run("parameter in query string that has an alias should be added to context", func(t *testing.T) {
		paramName := "l"
		alias := "limit"
		paramValue := "2"
		pValue := 2
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(alias) == nil {
				t.Fatalf("expected a value to be returned for '%s'", paramName)
			}
			tValue := cc.Value(alias).(int)
			if tValue != pValue {
				t.Errorf("expected the param '%s' to have value '%s', got '%v'", paramName, paramValue, tValue)
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs?"+paramName+"="+paramValue, nil)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
	})
	t.Run("a filter in query string that should be added to context", func(t *testing.T) {
		paramName := "_filters"
		paramValue := "2"
		convertValue := uint64(2)
		field := "id"
		operator := "eq"
		queryString := "/blogs?" + paramName + "[" + field + "][" + operator + "]=" + paramValue
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(paramName) == nil {
				t.Fatalf("expected a value to be returned for '%s'", paramName)
			}
			tValue := cc.Value(paramName)
			if tValue != nil {
				filters := tValue.(map[string]interface{})
				if filters == nil {
					t.Fatalf("expected filters got nil")
				}
				if filters[field].(*rest.FilterProperties).Field != field {
					t.Errorf("expected the filters field to be '%s', got '%s'", field, filters[field].(*rest.FilterProperties).Field)
				}
				if filters[field].(*rest.FilterProperties).Operator != operator {
					t.Errorf("expected the filters operator to be '%s', got '%s'", operator, filters[field].(*rest.FilterProperties).Operator)
				}
				if filters[field].(*rest.FilterProperties).Value.(uint64) != convertValue {
					t.Errorf("expected the filters value to be '%d', got '%d'", convertValue, filters[field].(*rest.FilterProperties).Value.(uint64))
				}
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, queryString, nil)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
	})
	t.Run("multiple filters in query string that should be added to context", func(t *testing.T) {
		paramName := "_filters"
		paramValue := "2"
		convertValue := uint64(2)
		paramValue2 := "5"
		field := "id"
		field2 := "title"
		operator := "eq"
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(paramName) == nil {
				t.Fatalf("expected a value to be returned for '%s'", paramName)
			}
			tValue := cc.Value(paramName)
			if tValue != nil {
				tValue := cc.Value(paramName)
				if tValue != nil {
					filters := tValue.(map[string]interface{})
					if filters[field].(*rest.FilterProperties).Field != field {
						t.Errorf("expected the filters field to be '%s', got '%s'", field, filters[field].(*rest.FilterProperties).Field)
					}
					if filters[field].(*rest.FilterProperties).Operator != operator {
						t.Errorf("expected the filters operator to be '%s', got '%s'", operator, filters[field].(*rest.FilterProperties).Operator)
					}
					if filters[field].(*rest.FilterProperties).Value.(uint64) != convertValue {
						t.Errorf("expected the filters value to be '%d', got '%d'", convertValue, filters[field].(*rest.FilterProperties).Value.(uint64))
					}
					if filters[field2].(*rest.FilterProperties).Field != field2 {
						t.Errorf("expected the filters field to be '%s', got '%s'", field2, filters[field2].(*rest.FilterProperties).Field)
					}
					if filters[field2].(*rest.FilterProperties).Operator != operator {
						t.Errorf("expected the filters operator to be '%s', got '%s'", operator, filters[field2].(*rest.FilterProperties).Operator)
					}
					if filters[field2].(*rest.FilterProperties).Value.(string) != paramValue2 {
						t.Errorf("expected the filters value to be '%s', got '%s'", paramValue2, filters[field2].(*rest.FilterProperties).Value.(string))
					}
				}
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		queryString := "/blogs?" + paramName + "[" + field + "][" + operator + "]=" + paramValue + "&" + paramName + "[" + field2 + "][" + operator + "]=" + paramValue2
		req := httptest.NewRequest(http.MethodGet, queryString, nil)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
	})
	t.Run("multiple filters with a filter that has multiple values in query string that should be added to context", func(t *testing.T) {
		paramName := "_filters"
		paramValue := "2"
		convertValue := uint64(2)
		value1 := "35"
		value2 := "54"
		value3 := "79"
		field := "id"
		field2 := "title"
		operator := "eq"
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(paramName) == nil {
				t.Fatalf("expected a value to be returned for '%s'", paramName)
			}
			tValue := cc.Value(paramName)
			if tValue != nil {
				tValue := cc.Value(paramName)
				if tValue != nil {
					filters := tValue.(map[string]interface{})
					if filters[field].(*rest.FilterProperties).Field != field {
						t.Errorf("expected the filters field to be '%s', got '%s'", field, filters[field].(*rest.FilterProperties).Field)
					}
					if filters[field].(*rest.FilterProperties).Operator != operator {
						t.Errorf("expected the filters operator to be '%s', got '%s'", operator, filters[field].(*rest.FilterProperties).Operator)
					}
					if filters[field].(*rest.FilterProperties).Value.(uint64) != convertValue {
						t.Errorf("expected the filters value to be '%d', got '%d'", convertValue, filters[field].(*rest.FilterProperties).Value.(uint64))
					}
					if filters[field2].(*rest.FilterProperties).Field != field2 {
						t.Errorf("expected the filters field to be '%s', got '%s'", field2, filters[field2].(*rest.FilterProperties).Field)
					}
					if filters[field2].(*rest.FilterProperties).Operator != operator {
						t.Errorf("expected the filters operator to be '%s', got '%s'", operator, filters[field2].(*rest.FilterProperties).Operator)
					}
					if len(filters[field2].(*rest.FilterProperties).Values) != 3 {
						t.Errorf("expected to get %d values but got %d,", 3, len(filters[field2].(*rest.FilterProperties).Values))
					}
				}
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		queryString := "/blogs?" + paramName + "[" + field + "][" + operator + "]=" + paramValue + "&" + paramName + "[" + field2 + "][" + operator + "]=" + value1 + "," + value2 + "," + value3
		req := httptest.NewRequest(http.MethodGet, queryString, nil)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
	})
	t.Run("a filter that has multiple values in query string that should be added to context", func(t *testing.T) {
		paramName := "_filters"
		value1 := "35"
		value2 := "54"
		value3 := "79"
		field := "id"
		operator := "eq"
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(paramName) == nil {
				t.Fatalf("expected a value to be returned for '%s'", paramName)
			}
			tValue := cc.Value(paramName)
			if tValue != nil {
				tValue := cc.Value(paramName)
				if tValue != nil {
					filters := tValue.(map[string]interface{})
					if filters[field].(*rest.FilterProperties).Field != field {
						t.Errorf("expected the filters field to be '%s', got '%s'", field, filters[field].(*rest.FilterProperties).Field)
					}
					if filters[field].(*rest.FilterProperties).Operator != operator {
						t.Errorf("expected the filters operator to be '%s', got '%s'", operator, filters[field].(*rest.FilterProperties).Operator)
					}
					if len(filters[field].(*rest.FilterProperties).Values) != 3 {
						t.Errorf("expected to get %d values but got %d,", 3, len(filters[field].(*rest.FilterProperties).Values))
					}
				}
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		queryString := "/blogs?" + paramName + "[" + field + "][" + operator + "]=" + value1 + "," + value2 + "," + value3
		req := httptest.NewRequest(http.MethodGet, queryString, nil)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
	})
	t.Run("json request payload should be added to context", func(t *testing.T) {
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Post)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value(context.PAYLOAD) == nil {
				t.Fatalf("expected a payload in context")
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		payload := &struct {
			Title string
		}{
			Title: "Lorem Ipsum",
		}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("unexpected error marshaling payload '%s'", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/blogs", bytes.NewBuffer(data))
		e.POST("/blogs", handler)
		e.ServeHTTP(resp, req)
	})

	t.Run("check that resonse type is added to the context", func(t *testing.T) {
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			responseType := cc.Value(context.RESPONSE_PREFIX + strconv.Itoa(http.StatusOK))
			if responseType != "application/json" {
				t.Errorf("expected the response type to be '%s', got '%s'", "application/json", responseType)
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs", nil)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
	})
}
