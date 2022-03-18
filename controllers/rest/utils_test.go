package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	weoscontext "github.com/wepala/weos/context"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	api "github.com/wepala/weos/controllers/rest"
)

func TestUtils_ConvertFormUrlEncodedToJson(t *testing.T) {

	t.Run("application/x-www-form-urlencoded content type", func(t *testing.T) {
		data := url.Values{}
		data.Set("title", "Test Blog")
		data.Set("url", "MyBlogUrl")

		body := strings.NewReader(data.Encode())

		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		payload, err := api.ConvertFormToJson(req, "application/x-www-form-urlencoded")
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
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		writer.WriteField("title", "Test Blog")
		writer.WriteField("url", "MyBlogUrl")
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		payload, err := api.ConvertFormToJson(req, "multipart/form-data")
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

}
func TestFiltersSplit(t *testing.T) {
	t.Run("testing splitfilters with multiple filters", func(t *testing.T) {
		queryString := "_filters[id][eq]=2&_filters[hi][ne]=5&_filters[and][in]=6"
		q0 := "_filters[id][eq]=2"
		q1 := "_filters[hi][ne]=5"
		q2 := "_filters[and][in]=6"
		arr := api.SplitFilters(queryString)
		if len(arr) != 3 {
			t.Fatalf("expected %d filters to be returned got %d", 3, len(arr))
		}
		if arr[0] != q0 {
			t.Errorf("expected first filter to be %s got %s", q0, arr[0])
		}
		if arr[1] != q1 {
			t.Errorf("expected first filter to be %s got %s", q1, arr[0])
		}
		if arr[2] != q2 {
			t.Errorf("expected first filter to be %s got %s", q2, arr[0])
		}

	})
	t.Run("testing splitfilters with no data", func(t *testing.T) {
		arr := api.SplitFilters("")
		if arr != nil {
			t.Errorf("expected filters to be nil got %s", arr[0])
		}
	})
	t.Run("testing splitfilter with no data", func(t *testing.T) {
		prop := api.SplitFilter("")
		if prop != nil {
			t.Errorf("expected filters properties to be nil got %s, %s, %s", prop.Field, prop.Value, prop.Operator)
		}

	})
	t.Run("testing splitfilter with a filter", func(t *testing.T) {
		queryString := "_filters[id][eq]=2"
		field := "id"
		operator := "eq"
		value := "2"
		prop := api.SplitFilter(queryString)
		if prop == nil {
			t.Fatalf("expected to get a property but go nil")
		}
		if prop.Field != field {
			t.Errorf("expected field to be %s got %s", field, prop.Field)
		}
		if prop.Operator != operator {
			t.Errorf("expected operator to be %s got %s", operator, prop.Operator)
		}
		if prop.Value != value {
			t.Errorf("expected value to be %s got %s", value, prop.Value)
		}

	})
	t.Run("testing splitfilter with a filter that has an array of values", func(t *testing.T) {
		queryString := "_filters[id][eq]=2,3,45"
		field := "id"
		operator := "eq"
		prop := api.SplitFilter(queryString)
		if prop == nil {
			t.Fatalf("expected to get a property but go nil")
		}
		if prop.Field != field {
			t.Errorf("expected field to be %s got %s", field, prop.Field)
		}
		if prop.Operator != operator {
			t.Errorf("expected operator to be %s got %s", operator, prop.Operator)
		}
		if len(prop.Values) != 3 {
			t.Errorf("expected value to be %d got %d", 3, len(prop.Values))
		}

	})
}

func TestGetJwkUrl(t *testing.T) {
	t.Run("valid url but no jwk url present", func(t *testing.T) {
		_, err := api.GetJwkUrl("https://google.com")
		if err == nil {
			t.Errorf("expected an error to returned for url: %s", "https://google.com")
		}

	})
	t.Run("invalid url", func(t *testing.T) {
		_, err := api.GetJwkUrl("jsisahudsdi")
		if err == nil {
			t.Errorf("expected an error to returned for url: %s", "jsisahudsdi")
		}

	})

}

func TestReturnContextValues(t *testing.T) {
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
	restAPI := &api.RESTAPI{}
	restAPI.SetEchoInstance(e)
	entityFactory := &EntityFactoryMock{
		NameFunc: func() string {
			return "Blog"
		},
		SchemaFunc: func() *openapi3.Schema {
			return swagger.Components.Schemas["Blog"].Value
		},
	}

	t.Run("invalid url", func(t *testing.T) {
		path := swagger.Paths.Find("/blogs")
		mw := api.Context(restAPI, nil, nil, nil, entityFactory, path, path.Get)
		mw1 := func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				ctx := c.Request().Context()
				values, err := api.ReturnContextValues(ctx, path.Get)
				if err != nil {
					t.Fatalf("unexpected error getting values from context")

				}
				ctx = context.WithValue(ctx, weoscontext.BASIC_RESPONSE, values)
				request := c.Request().WithContext(ctx)
				c.SetRequest(request)
				return next(c)
			}
		}

		e.GET("/blogs", func(c echo.Context) error {
			ctx := c.Request().Context()
			if values, ok := ctx.Value(weoscontext.BASIC_RESPONSE).(map[interface{}]interface{}); ok {
				//search for all values key
				if values != nil {
					if values["page"].(int) != 2 {
						t.Errorf("expected page to be %d got %d", 2, values["page"].(int))
					}
					if values["limit"].(int) != 10 {
						t.Errorf("expected limit to be %d got %d", 10, values["limit"].(int))
					}
				}
			}
			return nil
		}, mw, mw1)
		req := httptest.NewRequest(http.MethodGet, "/blogs?page=2", nil)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		response := rec.Result()
		defer response.Body.Close()

		if response.StatusCode != 200 {
			t.Errorf("expected the status code to be %d, got %d", 200, response.StatusCode)
		}
	})

}
