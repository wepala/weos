package rest_test

import (
	"errors"
	"fmt"
	api "github.com/wepala/weos/v2/rest"
	"testing"
)

//	func TestUtils_ConvertFormUrlEncodedToJson(t *testing.T) {
//		swagger, err := LoadConfig(t, "./fixtures/blog.yaml")
//		if err != nil {
//			t.Fatalf("unexpected error loading swagger config '%s'", err)
//		}
//
//		repository := &EntityRepositoryMock{SchemaFunc: func() *openapi3.Schema {
//			return swagger.Components.Schemas["Blog"].Value
//		}}
//		t.Run("application/x-www-form-urlencoded content type", func(t *testing.T) {
//			data := url.Values{}
//			data.Set("title", "Test Blog")
//			data.Set("url", "MyBlogUrl")
//
//			body := strings.NewReader(data.Encode())
//
//			req := httptest.NewRequest(http.MethodPost, "/blogs", body)
//			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
//
//			payload, err, _ := api.ConvertFormToJson(req, "application/x-www-form-urlencoded", repository, nil)
//			if err != nil {
//				t.Errorf("error converting form-urlencoded payload to json")
//			}
//
//			if payload == nil {
//				t.Errorf("error converting form-urlencoded payload to json")
//			}
//
//			var compare map[string]interface{}
//			err = json.Unmarshal(payload, &compare)
//			if err != nil {
//				t.Errorf("error unmashalling payload")
//			}
//
//			if compare["title"] != "Test Blog" {
//				t.Errorf("expected title: %s, got %s", "Test Blog", compare["title"])
//			}
//
//			if compare["url"] != "MyBlogUrl" {
//				t.Errorf("expected url: %s, got %s", "MyBlogUrl", compare["url"])
//			}
//		})
//
//		t.Run("multipart/form-data content type", func(t *testing.T) {
//			body := new(bytes.Buffer)
//			writer := multipart.NewWriter(body)
//			writer.WriteField("title", "Test Blog")
//			writer.WriteField("url", "MyBlogUrl")
//			writer.Close()
//
//			req := httptest.NewRequest(http.MethodPost, "/blogs", body)
//			req.Header.Set("Content-Type", writer.FormDataContentType())
//
//			payload, err, _ := api.ConvertFormToJson(req, "multipart/form-data", repository, nil)
//			if err != nil {
//				t.Errorf("error converting form-urlencoded payload to json")
//			}
//
//			if payload == nil {
//				t.Errorf("error converting form-urlencoded payload to json")
//			}
//
//			var compare map[string]interface{}
//			err = json.Unmarshal(payload, &compare)
//			if err != nil {
//				t.Errorf("error unmashalling payload")
//			}
//
//			if compare["title"] != "Test Blog" {
//				t.Errorf("expected title: %s, got %s", "Test Blog", compare["title"])
//			}
//
//			if compare["url"] != "MyBlogUrl" {
//				t.Errorf("expected url: %s, got %s", "MyBlogUrl", compare["url"])
//			}
//		})
//
// }
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
		if prop.Field != "" || prop.Operator != "" {
			t.Errorf("expected filters properties to be nil got %s, %s", prop.Field, prop.Operator)
		}

	})
	t.Run("testing splitfilter with a filter", func(t *testing.T) {
		queryString := "_filters[id][eq]=2"
		field := "id"
		operator := "eq"
		value := "2"
		prop := api.SplitFilter(queryString)
		if prop.Field == "" || prop.Operator == "" || prop.Value == "" {
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
		if prop.Field == "" || prop.Operator == "" || prop.Values == nil {
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

func TestSplitQueryParameters(t *testing.T) {
	t.Run("testing splitheaders", func(t *testing.T) {
		queryString := "_headers[familyName]=Last Name"
		header := "Last Name"
		field := "familyName"
		headerProp := api.SplitQueryParameters(queryString, "_headers")

		if headerProp == nil {
			t.Fatalf("expected to get a header property but go nil")
		}

		if headerProp.Field != field {
			t.Errorf("expected field to be %s got %s", field, headerProp.Field)
		}

		if headerProp.Value != header {
			t.Errorf("expected header to be %s got %s", header, headerProp.Value)
		}
	})

	t.Run("testing splitheaders with a '+' in the header value", func(t *testing.T) {
		queryString := "_headers[givenName]=First+Name"
		header := "First Name"
		field := "givenName"
		headerProp := api.SplitQueryParameters(queryString, "_headers")

		if headerProp == nil {
			t.Fatalf("expected to get a header property but go nil")
		}

		if headerProp.Field != field {
			t.Errorf("expected field to be %s got %s", field, headerProp.Field)
		}

		if headerProp.Value != header {
			t.Errorf("expected header to be %s got %s", header, headerProp.Value)
		}
	})
}

func TestConvertStringToType(t *testing.T) {
	tests := []struct {
		desiredType string
		format      string
		input       string
		output      interface{}
	}{
		{"number", "double", "100.5", 100.5},
		{"number", "float", "100.5", 100.5},
		{"number", "float", "1asdfasdf", errors.New("some error")},
		{"integer", "", "5", 5},
		{"integer", "int32", "5", int32(5)},
		{"integer", "int64", "5", int64(5)},
		{"boolean", "", "true", true},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("converting %s to type %s with format %s should return %v", tc.input, tc.desiredType, tc.format, tc.output), func(t *testing.T) {
			value, err := api.ConvertStringToType(tc.desiredType, tc.format, tc.input)
			//if the expected output is an error and one is not received then return test error
			if _, ok := tc.output.(error); ok {
				if err == nil {
					t.Error("expected error to be returned")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error converting '%s'", err)
				}

				if value != tc.output {
					t.Errorf("expected '%v', got '%v'", tc.output, value)
				}
			}
		})
	}
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

//func TestResolveResponseType(t *testing.T) {
//	swagger, err := LoadConfig(t, "./fixtures/blog.yaml")
//	if err != nil {
//		t.Fatalf("unable to load swagger: %s", err)
//	}
//	path := swagger.Paths.Find("/blogs/:id")
//	t.Run("any response type", func(t *testing.T) {
//		expectedContentType := "application/json"
//		contentType := api.ResolveResponseType("*/*", path.Get.Responses[strconv.Itoa(http.StatusOK)].Value.Content)
//		if contentType != expectedContentType {
//			t.Errorf("expected %s, got %s", expectedContentType, contentType)
//		}
//	})
//
//	t.Run("application types", func(t *testing.T) {
//		expectedContentType := "application/json"
//		contentType := api.ResolveResponseType("application/*", path.Get.Responses[strconv.Itoa(http.StatusOK)].Value.Content)
//		if contentType != expectedContentType {
//			t.Errorf("expected %s, got %s", expectedContentType, contentType)
//		}
//	})
//
//	t.Run("multiple types", func(t *testing.T) {
//		expectedContentType := "application/json"
//		contentType := api.ResolveResponseType("text/html, application/xhtml+xml, application/json, */*;q=0.8", path.Get.Responses[strconv.Itoa(http.StatusOK)].Value.Content)
//		if contentType != expectedContentType {
//			t.Errorf("expected %s, got %s", expectedContentType, contentType)
//		}
//	})
//
//	t.Run("test application/ld+json", func(t *testing.T) {
//		swagger, err = LoadConfig(t, "./fixtures/blog-json-ld.yaml")
//		if err != nil {
//			t.Fatalf("unable to load swagger: %s", err)
//		}
//		path = swagger.Paths.Find("/blogs")
//
//		expectedContentType := "application/ld+json"
//		contentType := api.ResolveResponseType("application/ld+json", path.Get.Responses[strconv.Itoa(http.StatusOK)].Value.Content)
//		if contentType != expectedContentType {
//			t.Errorf("expected %s, got %s", expectedContentType, contentType)
//		}
//	})
//
//	t.Run("test text/csv", func(t *testing.T) {
//		swagger, err = LoadConfig(t, "./fixtures/csv.yaml")
//		if err != nil {
//			t.Fatalf("unable to load swagger: %s", err)
//		}
//		path = swagger.Paths.Find("/customers")
//
//		expectedContentType := "text/csv"
//		contentType := api.ResolveResponseType("text/csv", path.Get.Responses[strconv.Itoa(http.StatusOK)].Value.Content)
//		if contentType != expectedContentType {
//			t.Errorf("expected %s, got %s", expectedContentType, contentType)
//		}
//	})
//}

func TestParseQueryFilters(t *testing.T) {
	logger := &LogMock{
		DebugfFunc: func(format string, args ...interface{}) {

		},
		ErrorfFunc: func(format string, args ...interface{}) {

		},
	}
	t.Run("should parse the query string into a map of filters", func(t *testing.T) {
		query := "_filters[account_id][eq]=123"
		filterOptions, err := api.ParseQueryFilters(query, logger)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		if len(filterOptions) != 1 {
			t.Fatalf("expected 1 filter option, got %d", len(filterOptions))
		}
		if filterOptions["account_id"].Field != "account_id" {
			t.Errorf("expected field to be account_id, got %s", filterOptions["account_id"].Field)
		}
		if filterOptions["account_id"].Operator != "eq" {
			t.Errorf("expected operator to be eq, got %s", filterOptions["account_id"].Operator)
		}
	})
}
