package rest_test

import (
	"bytes"
	"encoding/json"
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
