package rest_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wepala/weos/model"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	api "github.com/wepala/weos/controllers/rest"
)

func TestUtils_ConvertFormUrlEncodedToJson(t *testing.T) {

	t.Run("application/x-www-form-urlencoded content type", func(t *testing.T) {
		entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder("Blog", nil, nil)
		data := url.Values{}
		data.Set("title", "Test Blog")
		data.Set("url", "MyBlogUrl")

		body := strings.NewReader(data.Encode())

		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		payload, err, _ := api.ConvertFormToJson(req, "application/x-www-form-urlencoded", entityFactory, nil)
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
		entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder("Blog", nil, nil)
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		writer.WriteField("title", "Test Blog")
		writer.WriteField("url", "MyBlogUrl")
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		payload, err, _ := api.ConvertFormToJson(req, "multipart/form-data", entityFactory, nil)
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
