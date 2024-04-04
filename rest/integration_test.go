package rest_test

import (
	"encoding/json"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/v2/rest"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"golang.org/x/net/context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type RequestTest struct {
	Title          string
	URL            string
	Body           []byte
	Headers        map[string]string
	Method         string
	ExpectedStatus int
	ExpectedBody   map[string]interface{}
}

func TestIntegrations(t *testing.T) {
	//setup server
	var e *echo.Echo
	//ctxt := context.Background()
	os.Setenv("WEOS_PORT", "8681")
	os.Setenv("WEOS_SPEC", "./fixtures/blog.yaml")
	//use fx Module to start the server
	Receivers := func(commandDispatcher rest.CommandDispatcher) {
		handlers := []rest.CommandConfig{
			{
				Type: "CreateBlog",
				Handler: func(ctx context.Context, command *rest.Command, logger rest.Log, options *rest.CommandOptions) (response rest.CommandResponse, err error) {
					return rest.CommandResponse{
						Code: 400, //this is set deliberately for testing
						Body: map[string]interface{}{
							"title": "Test Blog",
						},
					}, nil
				},
			},
		}
		for _, handler := range handlers {
			commandDispatcher.AddSubscriber(handler)
		}
	}
	app := fxtest.New(t, fx.Invoke(Receivers), rest.Core, fx.Invoke(func(techo *echo.Echo) {
		e = techo
	}))
	defer app.RequireStop()
	app.RequireStart()
	tests := []RequestTest{
		{
			Title: "create organization",
			URL:   "/root",
			//crate json ld body for the request
			Body:           []byte(`{"@context": "http://schema.org","@type": "Organization","name": "Wepala", "@id": "/root"}`),
			Headers:        map[string]string{"Content-Type": "application/ld+json"},
			Method:         http.MethodPost,
			ExpectedStatus: 201,
			ExpectedBody:   map[string]interface{}{"@context": "http://schema.org", "@type": "Organization", "name": "Wepala", "@id": "/root"},
		},
		{
			Title: "create organization with a put request",
			URL:   "/root",
			//crate json ld body for the request
			Body:           []byte(`{"@context": "http://schema.org","@type": "Organization","name": "Wepala", "@id": "/put"}`),
			Headers:        map[string]string{"Content-Type": "application/ld+json"},
			Method:         http.MethodPut,
			ExpectedStatus: 201,
			ExpectedBody:   map[string]interface{}{"@context": "http://schema.org", "@type": "Organization", "name": "Wepala", "@id": "/put"},
		},
		{
			Title: "create organization with a patch request",
			URL:   "/root",
			//crate json ld body for the request
			Body:           []byte(`{"@context": "http://schema.org","@type": "Organization","name": "Wepala", "@id": "/patch"}`),
			Headers:        map[string]string{"Content-Type": "application/ld+json"},
			Method:         http.MethodPatch,
			ExpectedStatus: 201,
			ExpectedBody:   map[string]interface{}{"@context": "http://schema.org", "@type": "Organization", "name": "Wepala", "@id": "/patch"},
		},
		{
			Title: "create blog using command",
			URL:   "/blogs/1",
			//crate json ld body for the request
			Body:           []byte(`{"title": "Test Blog"}`),
			Headers:        map[string]string{"Content-Type": "application/json"},
			Method:         http.MethodPut,
			ExpectedStatus: 400,
			ExpectedBody:   map[string]interface{}{"title": "Test Blog"},
		},
	}

	for _, test := range tests {
		t.Run(test.Title, func(t *testing.T) {
			body := strings.NewReader(string(test.Body))
			req := httptest.NewRequest(test.Method, test.URL, body)
			req.Header.Set("Content-Type", test.Headers["Content-Type"])
			resp := httptest.NewRecorder()
			defer func() {
				err := resp.Result().Body.Close()
				if err != nil {
					t.Errorf("error closing response body: %s", err.Error())
				}
			}()
			e.ServeHTTP(resp, req)
			if resp.Code != test.ExpectedStatus {
				t.Errorf("expected status %d, got %d", test.ExpectedStatus, resp.Code)
			}
			resultPayload := make(map[string]interface{})
			err := json.Unmarshal(resp.Body.Bytes(), &resultPayload)
			if err != nil {
				t.Errorf("error unmarshalling response body: %s", err.Error())
			}
			for key, value := range test.ExpectedBody {
				if resultPayload[key] != value {
					t.Errorf("expected %s to be %v, got %v", key, value, resultPayload[key])
				}
			}
		})
	}

}
