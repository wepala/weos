package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/controllers/rest"
	"io/ioutil"
	"net/http/httptest"
	"os"
	"testing"
)

func TestOpenIDConnect_Validate(t *testing.T) {
	t.Run("validate token", func(t *testing.T) {
		var err error
		var swagger *openapi3.Swagger
		var rawJWT []byte
		swagger, err = LoadConfig(t, "fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading config '%s'", err)
		}

		rawRequest := httptest.NewRequest("PUT", "/blogs/1234", nil)
		rawJWT, err = ioutil.ReadFile("./fixtures/jwt/demo.jwt")
		if err != nil {
			t.Fatalf("unable to read jwt fixture '%s'", err)
		}
		rawRequest.Header.Add("Authorization", "Bearer "+string(rawJWT))
		rw := httptest.NewRecorder()

		e := echo.New()
		ctxt := e.NewContext(rawRequest, rw)

		authenticator, _ := new(rest.OpenIDConnect).FromSchema(swagger.Components.SecuritySchemes["WeAuth"].Value)
		result, _, userID, role, err := authenticator.Validate(ctxt)
		if err != nil {
			t.Fatalf("error authenticating '%s'", err)
		}

		if !result {
			t.Error("authentication failed")
		}

		if role != "staff" {
			t.Errorf("expected the role to be '%s', got '%s'", "staff", role)
		}

		if userID != "93b46dcd-baf1-41d2-99cc-449d02bfe195" {
			t.Errorf("expected the userID to be '%s', got '%s'", "93b46dcd-baf1-41d2-99cc-449d02bfe195", userID)
		}
	})
	t.Run("invalid token", func(t *testing.T) {
		var err error
		var swagger *openapi3.Swagger
		var rawJWT []byte
		swagger, err = LoadConfig(t, "fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading config '%s'", err)
		}

		rawRequest := httptest.NewRequest("PUT", "/blogs/1234", nil)
		rawJWT, err = ioutil.ReadFile("./fixtures/jwt/demo.jwt")
		if err != nil {
			t.Fatalf("unable to read jwt fixture '%s'", err)
		}
		rawRequest.Header.Add("Authorization", "Bearer "+string(rawJWT))
		rw := httptest.NewRecorder()

		e := echo.New()
		ctxt := e.NewContext(rawRequest, rw)

		authenticator, _ := new(rest.OpenIDConnect).FromSchema(swagger.Components.SecuritySchemes["Auth0"].Value)
		result, _, _, _, err := authenticator.Validate(ctxt)
		if result {
			t.Error("expected validation to fail")
		}
	})
}

func TestOAuth2_FromSchema(t *testing.T) {
	t.Skipf("need to figure out way to load certificates see https://wepala.atlassian.net/browse/WEOS-1520")
	t.Run("initialize oauth2 authenticator", func(t *testing.T) {
		swagger, err := LoadConfig(t, "fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading config '%s'", err)
		}
		authenticator, err := new(rest.OAuth2).FromSchema(swagger.Components.SecuritySchemes["Auth02"].Value)
		if tauthenticator, ok := authenticator.(rest.OAuth2); ok {
			if tauthenticator.Flows.AuthorizationCode.AuthorizationURL != swagger.Components.SecuritySchemes["Auth02"].Value.Flows.AuthorizationCode.AuthorizationURL {
				t.Errorf("expected authorization url to be '%s', got '%s'", swagger.Components.SecuritySchemes["Auth02"].Value.Flows.AuthorizationCode.AuthorizationURL, tauthenticator.Flows.AuthorizationCode.AuthorizationURL)
			}
		} else {
			t.Fatalf("expected OAuth2 authenticator")
		}
	})
}

func TestOAuth2_Authenticate(t *testing.T) {
	t.Run("authenticate valid token", func(t *testing.T) {
		t.Skipf("need to figure out way to load certificates see https://wepala.atlassian.net/browse/WEOS-1520")
		swagger, err := LoadConfig(t, "fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading config '%s'", err)
		}

		rawRequest := httptest.NewRequest("POST", "/blogs/1234", nil)
		token, ok := os.LookupEnv("OAUTH_TEST_KEY")
		if !ok {
			t.Fatal("test requires token set in 'OAUTH_TEST_KEY' environment variable")
		}
		rawRequest.Header.Add("Authorization", "Bearer "+token)
		rw := httptest.NewRecorder()

		e := echo.New()
		ctxt := e.NewContext(rawRequest, rw)

		authenticator, _ := new(rest.OAuth2).FromSchema(swagger.Components.SecuritySchemes["Auth02"].Value)
		result, _, _, _, err := authenticator.Validate(ctxt)
		if err != nil {
			t.Fatalf("error authenticating '%s'", err)
		}

		if !result {
			t.Error("authentication failed")
		}
	})
}
