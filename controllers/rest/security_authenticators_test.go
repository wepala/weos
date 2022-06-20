package rest_test

import (
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/controllers/rest"
	"net/http/httptest"
	"os"
	"testing"
)

func TestOAuth2_FromSchema(t *testing.T) {
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
		result, err := authenticator.Validate(ctxt)
		if err != nil {
			t.Fatalf("error authenticating '%s'", err)
		}

		if !result {
			t.Error("authentication failed")
		}
	})
}
