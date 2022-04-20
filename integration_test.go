package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestIntegration_UploadOnProperty(t *testing.T) {
	os.Remove("./files/test.csv")
	os.Remove("test.db")
	content, err := ioutil.ReadFile("./controllers/rest/fixtures/blog-x-upload.yaml")
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

	t.Run("upload a file of valid size as property", func(t *testing.T) {
		//os.Remove("./files/test.csv")
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)

		writer.WriteField("title", "this is my title")
		writer.WriteField("url", "this is my url")

		file, err := os.Open("./controllers/rest/fixtures/files/test.csv")
		if err != nil {
			t.Error(err)
		}
		defer file.Close()

		part, err := writer.CreateFormFile("description", "test.csv")
		io.Copy(part, file)

		writer.Close()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		header = http.Header{}
		header.Set("Content-Type", writer.FormDataContentType())
		req.Header = header
		req.Close = true
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusCreated {
			t.Fatalf("expected to get status %d creating fixtures, got %d", http.StatusCreated, resp.Result().StatusCode)
		}
		os.Remove("./files/test.csv")
	})

	t.Run("upload a file of invalid size as property", func(t *testing.T) {
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)

		writer.WriteField("title", "this is my title")
		writer.WriteField("url", "this is my url")

		file, err := os.Open("./controllers/rest/fixtures/files/test20.csv")
		if err != nil {
			t.Error(err)
		}
		defer file.Close()

		part, err := writer.CreateFormFile("description", "test20.csv")
		io.Copy(part, file)

		writer.Close()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		header = http.Header{}
		header.Set("Content-Type", writer.FormDataContentType())
		req.Header = header
		req.Close = true
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("expected to get status %d creating fixtures, got %d", http.StatusBadRequest, resp.Result().StatusCode)
		}
		os.Remove("./files/test20.csv")
	})

	t.Run("file already exists please rename", func(t *testing.T) {

		file, err := os.Open("./controllers/rest/fixtures/files/test.csv")
		if err != nil {
			t.Error(err)
		}
		defer file.Close()

		buf := bytes.NewBuffer(nil)
		if _, err := io.Copy(buf, file); err != nil {
			t.Fatalf("error creating buffer")
		}

		//Checks if folder exists and creates it if not
		_, err = os.Stat("./files")
		if os.IsNotExist(err) {
			err := os.MkdirAll("./files", os.ModePerm)
			if err != nil {
				t.Fatalf("error creating directory")
			}
		}

		filePath := "./files/test.csv"

		//Checks if file exists in folder and creates it if not
		_, err = os.Stat(filePath)

		if os.IsNotExist(err) {
			os.WriteFile(filePath, buf.Bytes(), os.ModePerm)
		}

		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)

		writer.WriteField("title", "this is my title 1")
		writer.WriteField("url", "this is my url 1")

		file, err = os.Open("./controllers/rest/fixtures/files/test.csv")
		if err != nil {
			t.Error(err)
		}
		defer file.Close()

		part, err := writer.CreateFormFile("description", "test.csv")
		io.Copy(part, file)

		writer.Close()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		header = http.Header{}
		header.Set("Content-Type", writer.FormDataContentType())
		req.Header = header
		req.Close = true
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("expected to get status %d creating fixtures, got %d", http.StatusCreated, resp.Result().StatusCode)
		}
		os.Remove("./files/test.csv")
	})
}

func TestIntegration_UploadOnEndpoint(t *testing.T) {
	os.Remove("./files/test.csv")
	os.Remove("test.db")
	content, err := ioutil.ReadFile("./controllers/rest/fixtures/blog-x-upload.yaml")
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

	t.Run("upload a file of valid size as endpoint", func(t *testing.T) {
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)

		file, err := os.Open("./controllers/rest/fixtures/files/test.csv")
		if err != nil {
			t.Error(err)
		}
		defer file.Close()

		part, err := writer.CreateFormFile("description", "test.csv")
		io.Copy(part, file)

		writer.Close()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/files", body)
		header = http.Header{}
		header.Set("Content-Type", writer.FormDataContentType())
		req.Header = header
		req.Close = true
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusCreated {
			t.Fatalf("expected to get status %d creating fixtures, got %d", http.StatusOK, resp.Result().StatusCode)
		}
	})
	os.Remove("./files/test.csv")

	t.Run("upload a file of invalid size as endpoint", func(t *testing.T) {
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)

		file, err := os.Open("./controllers/rest/fixtures/files/test20.csv")
		if err != nil {
			t.Error(err)
		}
		defer file.Close()

		part, err := writer.CreateFormFile("description", "test20.csv")
		io.Copy(part, file)

		writer.Close()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", body)
		header = http.Header{}
		header.Set("Content-Type", writer.FormDataContentType())
		req.Header = header
		req.Close = true
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("expected to get status %d creating fixtures, got %d", http.StatusBadRequest, resp.Result().StatusCode)
		}
	})

	os.Remove("./files/test20.csv")
}

func TestIntegration_FilteringByCamelCase(t *testing.T) {
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

	//create bach authors for tests
	authors := []map[string]interface{}{
		{
			"firstName": "first",
			"lastName":  "first",
		},
		{
			"firstName": "second",
			"lastName":  "second",
		},
		{
			"firstName": "third",
			"lastName":  "third",
		},
	}
	reqBytes, err := json.Marshal(authors)
	if err != nil {
		t.Fatalf("error setting up request %s", err)
	}
	body := bytes.NewReader(reqBytes)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/authors/batch", body)
	header = http.Header{}
	header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header = header
	req.Close = true
	e.ServeHTTP(resp, req)

	if resp.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected to get status %d creating fixtures, got %d", http.StatusCreated, resp.Result().StatusCode)
	}

	t.Run("filtering by using the name in the spec file", func(t *testing.T) {
		queryString := "_filters[firstName][eq]=first"
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/authors?"+queryString, nil)
		req.Close = true
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusOK {
			t.Fatalf("expected to get status %d getting item, got %d", http.StatusOK, resp.Result().StatusCode)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		var resultAuthor api.ListApiResponse
		err = json.Unmarshal(bodyBytes, &resultAuthor)
		if err != nil {
			t.Errorf("unexpected error : got error unmarshalling response body, %s", err)
		}
		if len(resultAuthor.Items) != 1 {
			t.Errorf("expected number of items to be %d got %d ", 1, len(resultAuthor.Items))
		}

	})
}
