package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
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

func TestIntegration_Update(t *testing.T) {
	os.Remove("kollectables.db")
	content, err := ioutil.ReadFile("./controllers/rest/fixtures/kollectables-api.yaml")
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
	profile := map[string]interface{}{
		"id":       "123",
		"username": "",
		"email":    "test.com",
		"twitchId": "123456",
	}

	reqBytes, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("error setting up request %s", err)
	}
	body := bytes.NewReader(reqBytes)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/profile", body)
	header = http.Header{}
	header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header = header
	req.Close = true
	e.ServeHTTP(resp, req)

	if resp.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected to get status %d creating fixtures, got %d", http.StatusCreated, resp.Result().StatusCode)
	}

	etag := resp.Header().Get("Etag")
	weosID, _ := api.SplitEtag(etag)

	projection, err := tapi.GetProjection("Default")
	if err != nil {
		t.Fatal(err)
	}

	entityFactory := tapi.GetEntityFactories()

	newContext := context.Background()

	existingEnt, err := projection.GetContentEntity(newContext, entityFactory["Profile"], weosID)
	if err != nil {
		t.Fatal(err)
	}

	newContext = context.WithValue(newContext, weoscontext.WEOS_ID, existingEnt.ID)
	newContext = context.WithValue(newContext, weoscontext.SEQUENCE_NO, existingEnt.SequenceNo)

	commandDispatcher, err := tapi.GetCommandDispatcher("Default")
	if err != nil {
		t.Fatal(err)
	}

	eventSource, err := tapi.GetEventStore("Default")
	if err != nil {
		t.Fatal(err)
	}

	data := map[string]interface{}{}
	x, _ := json.Marshal(existingEnt.Property)
	_ = json.Unmarshal(x, &data)
	entityID := data["id"]
	data["username"] = "FooBar"
	newPayload, _ := json.Marshal(data)

	newContext = context.WithValue(newContext, weoscontext.ENTITY_FACTORY, entityFactory["Profile"])
	newContext = context.WithValue(newContext, "id", entityID)

	err = commandDispatcher.Dispatch(newContext, model.Update(newContext, newPayload, entityFactory["Profile"].Name()), eventSource, projection, tapi.EchoInstance().Logger)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove("kollectables.db")
}

func TestIntegration_UploadOnProperty(t *testing.T) {
	os.Remove("./files")
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
	})
	os.Remove("./files")
	os.Remove("test.db")

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
	})
}

func TestIntegration_UploadOnEndpoint(t *testing.T) {
	os.Remove("./files")
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
	os.Remove("./files")
	os.Remove("test.db")

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
}
