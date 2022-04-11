package rest_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	context1 "golang.org/x/net/context"
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
					if _, ok := filters[field].(*rest.FilterProperties).Value.(uint64); !ok {
						t.Fatalf("expected '%s' to be uint64", field)
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
	t.Run("x-content extension should be used to add data to the request context", func(t *testing.T) {
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value("page") == nil {
				t.Fatalf("expected a page in context")
			}
			if cc.Value("limit") == nil {
				t.Fatalf("expected a limit in context")
			}
			if cc.Value("_sorts") == nil {
				t.Fatalf("expected a sort in context")
			}
			if cc.Value("_filters") == nil {
				t.Fatalf("expected a filter in context")
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs", nil)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
	})
	t.Run("the request parameter value should take preference over x-context parameters values", func(t *testing.T) {
		path := swagger.Paths.Find("/blogs/:id")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			if cc.Value("id").(string) != "123" {
				t.Fatalf("expected an id in context to be %s got %s", "123", cc.Value("id").(string))
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
		req := httptest.NewRequest(http.MethodGet, "/blogs/123", bytes.NewBuffer(data))
		e.GET("/blogs/:id", handler)
		e.ServeHTTP(resp, req)
	})

	t.Run("add operationId to context", func(t *testing.T) {
		path := swagger.Paths.Find("/blogs")
		mw := rest.Context(restApi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			value := cc.Value(context.OPERATION_ID)
			if value == nil {
				t.Fatalf("expected the operation id to have a value")
			}
			if value.(string) != "Get Blogs" {
				t.Fatalf("expected the operation id to be Get Blogs")
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs", nil)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
	})
	t.Run("add session data to context when security is declared global", func(t *testing.T) {
		//set up so the api can have what is needed
		aapi, err := rest.New("./fixtures/session.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context1.TODO(), aapi, aapi.Swagger)
		if err != nil {
			t.Fatalf("unexpected error opening gorm db connection")
		}
		gormDB, err := aapi.GetGormDBConnection("Default")
		if err != nil {
			t.Fatalf("unexpected error getting gorm db connection")
		}
		defaultProjection, err := projections.NewProjection(context1.Background(), gormDB, e.Logger)
		if err != nil {
			t.Fatalf("unexpected error instantiating new projection")
		}
		aapi.SetEchoInstance(e)
		aapi.RegisterProjection("Default", defaultProjection)
		_, err = rest.Security(context1.Background(), aapi, aapi.Swagger)
		if err != nil {
			t.Fatalf("unexpected error setting up the intialization of session")
		}
		path := aapi.Swagger.Paths.Find("/blogs")
		mw := rest.Context(aapi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			value := cc.Value("id")
			if value == nil {
				t.Fatalf("expected the id to have a value")
			}
			if value.(int) != 1234 {
				t.Fatalf("expected the operation id to be 123")
			}
			value = cc.Value("oauth")
			if value == nil {
				t.Fatalf("expected the oauth to have a value")
			}
			if value.(string) != "oath|dhhbsgy" {
				t.Fatalf("expected the operation id to be oath|dhhbsgy")
			}
			return nil
		})
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs", nil)
		sessionStore := aapi.GetSessionStore()
		session, err := sessionStore.Get(req, "JSESSIONID")
		if err != nil {
			t.Fatalf("unexpected error getting session")
		}
		session.Values["id"] = int(1234)
		session.Values["oauth"] = "oath|dhhbsgy"
		sessionStore.Save(req, resp, session)
		c := &http.Cookie{Name: "JSESSIONID", Value: session.ID}
		req.AddCookie(c)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("unexpected error, expected status code to be %d got %d", http.StatusOK, resp.Code)
		}
		os.Remove("test.db")
	})
	t.Run("add session data to context when security is declared on a path", func(t *testing.T) {
		//set up so the api can have what is needed
		sessionName := "JSESSIONID"
		aapi, err := rest.New("./fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		_, err = rest.SQLDatabase(context1.TODO(), aapi, aapi.Swagger)
		if err != nil {
			t.Fatalf("unexpected error opening gorm db connection")
		}
		gormDB, err := aapi.GetGormDBConnection("Default")
		if err != nil {
			t.Fatalf("unexpected error getting gorm db connection")
		}
		defaultProjection, err := projections.NewProjection(context1.Background(), gormDB, e.Logger)
		if err != nil {
			t.Fatalf("unexpected error instantiating new projection")
		}
		aapi.SetEchoInstance(e)
		aapi.RegisterProjection("Default", defaultProjection)
		_, err = rest.Security(context1.Background(), aapi, aapi.Swagger)
		if err != nil {
			t.Fatalf("unexpected error setting up the intialization of session")
		}
		path := aapi.Swagger.Paths.Find("/blogs")
		mw := rest.Context(aapi, nil, nil, nil, entityFactory, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that certain parameters are in the context
			cc := ctxt.Request().Context()
			value := cc.Value("active")
			if value == nil {
				t.Fatalf("expected the active to have a value")
			}
			if value.(bool) != true {
				t.Fatalf("expected the operation active to be true")
			}
			value = cc.Value("oauth")
			if value == nil {
				t.Fatalf("expected the oauth to have a value")
			}
			if value.(string) != "oath|dhhbsgy" {
				t.Fatalf("expected the operation id to be oath|dhhbsgy")
			}
			return nil
		})
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs", nil)
		sessionStore := aapi.GetSessionStore()
		session, err := sessionStore.Get(req, sessionName)
		if err != nil {
			t.Fatalf("unexpected error getting session")
		}
		session.Values["active"] = true
		session.Values["oauth"] = "oath|dhhbsgy"
		sessionStore.Save(req, resp, session)
		c := &http.Cookie{Name: sessionName, Value: session.ID}
		req.AddCookie(c)
		e.GET("/blogs", handler)
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("unexpected error, expected status code to be %d got %d", http.StatusOK, resp.Code)
		}
		//testing the GetSession
		session1, err := aapi.GetSession(session.ID, sessionName)
		if err != nil {
			t.Errorf("unexpected error getting back session: %s", err)
		}
		session1.Values["author"] = "fun man"
		session1.Values["owner"] = "funTick man"
		sessionStore.Save(&http.Request{}, &httptest.ResponseRecorder{}, session1)
		session2, err := aapi.GetSession(session.ID, sessionName)
		if err != nil {
			t.Errorf("unexpected error getting back session: %s", err)
		}
		if session2.Values["author"] == nil || session2.Values["author"].(string) != "fun man" {
			t.Errorf("unexpected error session value doesnt contain author value fun man")
		}
		if session2.Values["owner"] == nil || session2.Values["owner"].(string) != "funTick man" {
			t.Errorf("unexpected error session value doesnt contain owner value funTick man")
		}
		os.Remove("test.db")
	})
	t.Run("when security is declared global and security is off a path no error is thrown", func(t *testing.T) {
		//set up so the api can have what is needed
		aapi, err := rest.New("./fixtures/session.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		path := aapi.Swagger.Paths.Find("/health")
		mw := rest.Context(aapi, nil, nil, nil, entityFactory, path, path.Get)
		handler := rest.HealthCheck(aapi, nil, nil, nil, nil)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		e.GET("/health", handler, mw)
		e.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("unexpected error, expected status code to be %d got %d", http.StatusOK, resp.Code)
		}
		os.Remove("test.db")
	})
	t.Run("when security is declared global and x-session wasnt specify a warning is thrown", func(t *testing.T) {
		//set up so the api can have what is needed
		aapi, err := rest.New("./fixtures/session.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		ec := aapi.EchoInstance()
		buf := bytes.Buffer{}
		ec.Logger.SetOutput(&buf)
		ec.Logger.SetLevel(2)
		path := aapi.Swagger.Paths.Find("/blogs")
		mw := rest.Context(aapi, nil, nil, nil, entityFactory, path, path.Post)
		handler := mw(func(ctxt echo.Context) error {
			cc := ctxt.Request().Context()
			if cc.Value(context.PAYLOAD) == nil {
				t.Fatalf("expected a payload in context")
			}
			return nil
		})
		payload := &struct {
			Title string
		}{
			Title: "Lorem Ipsum",
		}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("unexpected error marshaling payload '%s'", err)
		}
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", bytes.NewBuffer(data))
		req.Header.Set("Content-Type", "application/json")
		ec.POST("/blogs", handler)
		ec.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Errorf("unexpected error, expected status code to be %d got %d", http.StatusOK, resp.Code)
		}
		if !strings.Contains(buf.String(), "no x-session extension was found") {
			t.Errorf("expected a warning: no x-session extension was found got %s", buf.String())
		}
		os.Remove("test.db")
	})

}

type Transaction struct {
	Title        string   `json:"title"`
	Titles       []string `json:"titles"`
	Url          string   `json:"url"`
	Amount       float64  `json:"amount"`
	Amount64     float64  `json:"amount64"`
	AmountDouble float64  `json:"amountDouble"`
	Count        int      `json:"count"`
	Count32      int      `json:"count32"`
	Count64      int      `json:"count64"`
}

func TestContext_ConvertFormUrlEncodedToJson(t *testing.T) {

	t.Run("application/x-www-form-urlencoded content type", func(t *testing.T) {
		entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder("Transaction", nil, nil)

		data := url.Values{}
		data.Set("title", "Test Blog")
		data.Set("url", "MyBlogUrl")

		body := strings.NewReader(data.Encode())

		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		payload, err, _ := rest.ConvertFormToJson(req, "application/x-www-form-urlencoded", entityFactory, nil)
		if err != nil {
			t.Errorf("error converting form-urlencoded payload to json")
		}

		if payload == nil {
			t.Errorf("error converting form-urlencoded payload to json")
		}

		var compare map[string]interface{}
		err = json.Unmarshal(payload, &compare)
		if err != nil {
			t.Errorf("error unmashalling payload")
		}

		if compare["title"] != "Test Blog" {
			t.Errorf("expected title: %s, got %s", "Test Blog", compare["title"])
		}

		if compare["url"] != "MyBlogUrl" {
			t.Errorf("expected url: %s, got %s", "MyBlogUrl", compare["url"])
		}
	})

	t.Run("multipart/form-data content type", func(t *testing.T) {
		entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder("Transaction", nil, nil)

		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		writer.WriteField("title", "Test Blog")
		writer.WriteField("url", "MyBlogUrl")
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		payload, err, _ := rest.ConvertFormToJson(req, "multipart/form-data", entityFactory, nil)
		if err != nil {
			t.Errorf("error converting form-urlencoded payload to json")
		}

		if payload == nil {
			t.Errorf("error converting form-urlencoded payload to json")
		}

		var compare map[string]interface{}
		err = json.Unmarshal(payload, &compare)
		if err != nil {
			t.Errorf("error unmashalling payload")
		}

		if compare["title"] != "Test Blog" {
			t.Errorf("expected title: %s, got %s", "Test Blog", compare["title"])
		}

		if compare["url"] != "MyBlogUrl" {
			t.Errorf("expected url: %s, got %s", "MyBlogUrl", compare["url"])
		}
	})

	spec := `openapi: 3.0.3
info:
  title: Example Application
  description: Payment page
  version: 1.0.0
servers:
  - url: 'http://localhost:8682'
    description: Local Development environment
x-weos-config:
  database:
    driver: sqlite3
    database: payment
components:
  schemas:
    Transaction:
      type: object
      properties:
        id:
          type: string
          format: ksuid
        title:
          type: string
        amount:
          type: number
        amountDouble:
          type: number
          format: double
        amount64:
          type: number
          format: float
        count:
          type: integer
        count32:
          type: integer
          format: int32
        count64:
          type: integer
          format: int64
      x-identifier:
        - id
      required:
        - invoice
paths:
  /:
    get:
      operationId: Show Form
      responses:
        200:
          description: Homepage
          x-file: forms/form.html
    post:
      operationId: Create Transaction
      requestBody:
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/Transaction"
          multipart/form-data:
            schema:
              $ref: "#/components/schemas/Transaction"
      responses:
        201:
          description: Tranasaction created
  /health:
    get:
      responses:
        200:
          description: Basic response
          content:
            text/html:
              example: |
                <html>
                  <head>
                    <title>Health Check</title>
                  </head>
                  <body>Health Page</title>
                </html>
  
`

	t.Run("multipart/form-data content type with numbers", func(t *testing.T) {

		fixture, err := rest.New(spec)
		if err != nil {
			t.Fatalf("unexpected error initializing api fixture '%s'", err)
		}

		var tschema *openapi3.SchemaRef
		var ok bool

		if tschema, ok = fixture.Swagger.Components.Schemas["Transaction"]; !ok {
			t.Fatal("unexpected error Transaction schema doesn't exist")
		}

		contentType := "Transaction"
		entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, tschema.Value, fixture.Schemas["Transaction"])

		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		writer.WriteField("title", "Test Transaction")
		writer.WriteField("amount", "100.05")
		writer.WriteField("amountDouble", "100.05")
		writer.WriteField("amount64", "100.05")
		writer.WriteField("count", "5")
		writer.WriteField("count32", "5")
		writer.WriteField("count64", "5")
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		payload, err, _ := rest.ConvertFormToJson(req, "multipart/form-data", entityFactory, nil)
		if err != nil {
			t.Errorf("error converting form-urlencoded payload to json")
		}

		if payload == nil {
			t.Errorf("error converting form-urlencoded payload to json")
		}

		var compare Transaction
		err = json.Unmarshal(payload, &compare)
		if err != nil {
			t.Errorf("error unmashalling payload")
		}

		if compare.Title != "Test Transaction" {
			t.Errorf("expected title to be '%s', got '%s'", "Test Transaction", compare.Title)
		}

		if compare.Amount != 100.05 {
			t.Errorf("expected amount to be %f, got '%v'", 100.05, compare.Amount)
		}

		if compare.Count != 5 {
			t.Errorf("expected amount to be %d, got %v", 5, compare.Count)
		}
	})
	t.Run("application/x-www-form-urlencoded content type with numbers", func(t *testing.T) {

		fixture, err := rest.New(spec)
		if err != nil {
			t.Fatalf("unexpected error initializing api fixture '%s'", err)
		}
		var tschema *openapi3.SchemaRef
		var ok bool

		if tschema, ok = fixture.Swagger.Components.Schemas["Transaction"]; !ok {
			t.Fatal("unexpected error Transaction schema doesn't exist")
		}

		contentType := "Transaction"
		entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, tschema.Value, fixture.Schemas["Transaction"])

		data := url.Values{}
		data.Set("title", "Test Blog")

		data.Set("title", "Test Transaction")
		data.Set("amount", "100.05")
		data.Set("amountDouble", "100.05")
		data.Set("amount64", "100.05")
		data.Set("count", "5")
		data.Set("count32", "5")
		data.Set("count64", "5")

		body := strings.NewReader(data.Encode())

		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		payload, err, _ := rest.ConvertFormToJson(req, "application/x-www-form-urlencoded", entityFactory, nil)
		if err != nil {
			t.Errorf("error converting form-urlencoded payload to json")
		}

		if payload == nil {
			t.Errorf("error converting form-urlencoded payload to json")
		}

		var compare Transaction
		err = json.Unmarshal(payload, &compare)
		if err != nil {
			t.Errorf("error unmashalling payload")
		}

		if compare.Title != "Test Transaction" {
			t.Errorf("expected title to be '%s', got '%s'", "Test Transaction", compare.Title)
		}

		if compare.Amount != 100.05 {
			t.Errorf("expected amount to be %f, got '%v'", 100.05, compare.Amount)
		}

		if compare.Count != 5 {
			t.Errorf("expected amount to be %d, got %v", 5, compare.Count)
		}
	})

	t.Run("multipart/form-data content type with array ", func(t *testing.T) {
		entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder("Transaction", nil, nil)

		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		writer.WriteField("titles[]", "Test Transaction")
		writer.WriteField("titles[]", "MyBlogUrl")
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		payload, err, _ := rest.ConvertFormToJson(req, "multipart/form-data", entityFactory, nil)
		if err != nil {
			t.Errorf("error converting form-urlencoded payload to json")
		}

		if payload == nil {
			t.Errorf("error converting form-urlencoded payload to json")
		}

		var compare Transaction
		err = json.Unmarshal(payload, &compare)
		if err != nil {
			t.Errorf("error unmashalling payload")
		}

		if len(compare.Titles) != 2 {
			t.Fatalf("expected %d titles, got %d", 2, len(compare.Titles))
		}

		if compare.Titles[0] != "Test Transaction" {
			t.Errorf("expected title to be '%s', got '%s'", "Test Transaction", compare.Titles[0])
		}

		if compare.Titles[1] != "MyBlogUrl" {
			t.Errorf("expected title to be '%s', got '%s'", "MyBlogUrl", compare.Titles[1])
		}

	})
}
