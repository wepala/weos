package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	api "github.com/wepala/weos/controllers/rest"
)

func TestIntegration_XUnique(t *testing.T) {
	dropDB()
	content, err := ioutil.ReadFile("./controllers/rest/fixtures/blog-integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	contentString := string(content)
	contentString = fmt.Sprintf(contentString, dbconfig.Database, dbconfig.Driver, dbconfig.Host, dbconfig.Password, dbconfig.User, dbconfig.Port)

	tapi, err := api.New(contentString)
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}
	err = tapi.Initialize(context.TODO())
	if err != nil {
		t.Fatalf("un expected error loading spec '%s'", err)
	}

	e := tapi.EchoInstance()

	//create bach blogs for tests
	blogs := []map[string]interface{}{
		{
			"title":       "first",
			"description": "first",
			"url":         "first.com",
		},
		{
			"title":       "second",
			"description": "second",
			"url":         "second.com",
		},
		{
			"title":       "third",
			"description": "third",
			"url":         "third.com",
		},
	}
	reqBytes, err := json.Marshal(blogs)
	if err != nil {
		t.Fatalf("error setting up request %s", err)
	}
	body := bytes.NewReader(reqBytes)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/blogs/batch", body)
	header = http.Header{}
	header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header = header
	req.Close = true
	e.ServeHTTP(resp, req)

	if resp.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected to get status %d creating fixtures, got %d", http.StatusCreated, resp.Result().StatusCode)
	}

	t.Run("Create an item with clashing unique field", func(t *testing.T) {
		blog := map[string]interface{}{

			"title":       "first blog",
			"description": "first blog ever",
			"url":         "first.com",
		}

		reqBytes, err := json.Marshal(blog)
		if err != nil {
			t.Fatalf("error setting up request %s", err)
		}
		body := bytes.NewReader(reqBytes)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		header = http.Header{}
		header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header = header
		req.Close = true
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("expected to get status %d creating item, got %d", http.StatusBadRequest, resp.Result().StatusCode)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		resultString := string(bodyBytes)
		if !strings.Contains(resultString, "should be unique") {
			t.Fatalf("expexted to get a unique error, got '%s'", resultString)
		}

	})

	t.Run("Update a field so unique field clashes", func(t *testing.T) {
		blog := map[string]interface{}{
			"id":          2,
			"title":       "second",
			"description": "second",
			"url":         "third.com",
		}

		reqBytes, err := json.Marshal(blog)
		if err != nil {
			t.Fatalf("error setting up request %s", err)
		}
		body := bytes.NewReader(reqBytes)
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/blogs/2", body)
		header = http.Header{}
		header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header = header
		req.Close = true
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("expected to get status %d updating item, got %d", http.StatusBadRequest, resp.Result().StatusCode)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		resultString := string(bodyBytes)
		if !strings.Contains(resultString, "should be unique") {
			t.Fatalf("expexted to get a unique error, got '%s'", resultString)
		}
	})

	dropDB()
}
