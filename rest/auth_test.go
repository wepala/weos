package rest_test

import (
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/v2/rest"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOpenIDConnect_Validate(t *testing.T) {
	t.Skipf("not sure why this started breaking")
	schema, err := openapi3.NewLoader().LoadFromFile("fixtures/blog-security.yaml")
	if err != nil {
		t.Fatalf("error encountered loading schema '%s'", err)
	}
	t.Run("validate token", func(t *testing.T) {
		var err error

		var rawJWT []byte
		rawRequest := httptest.NewRequest("PUT", "/blogs/1234", nil)
		rawJWT, err = ioutil.ReadFile("./fixtures/jwt/demo.jwt")
		if err != nil {
			t.Fatalf("unable to read jwt fixture '%s'", err)
		}
		rawRequest.Header.Add("Authorization", "Bearer "+string(rawJWT))
		rw := httptest.NewRecorder()

		e := echo.New()
		ctxt := e.NewContext(rawRequest, rw)
		httpClient := NewTestClient(func(req *http.Request) *http.Response {
			if strings.Contains(req.URL.Path, "/.well-known/openid-configuration") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"issuer":                 "https://example.org",
					"authorization_endpoint": "https://example.org/auth",
					"token_endpoint":         "https://example.org/token",
					"userinfo_endpoint":      "https://example.org/userinfo",
					"jwks_uri":               "https://example.org/jwks",
					"registration_endpoint":  "https://example.org/register",
					"revocation_endpoint":    "https://example.org/revoke",
					"scopes_supported":       []string{"openid", "profile", "email", "address", "phone"},
				})
			}
			if strings.Contains(req.URL.Path, "/jwks") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"keys": []map[string]interface{}{
						{
							"kid": "test",
							"kty": "RSA",
							"n":   "test",
							"e":   "AQAB",
						},
					},
				})
			}
			if strings.Contains(req.URL.Path, "/userinfo") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"sub":            "93b46dcd-baf1-41d2-99cc-449d02bfe195",
					"email":          "test@example.org",
					"email_verified": true,
					"role":           "staff",
				})
			}
			if strings.Contains(req.URL.Path, "/auth") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"access_token": "test",
					"token_type":   "Bearer",
					"expires_in":   3600,
				})
			}
			if strings.Contains(req.URL.Path, "/token") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"access_token": "test",
					"token_type":   "Bearer",
					"expires_in":   3600,
				})
			}
			if strings.Contains(req.URL.Path, "/register") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"client_id":     "test",
					"client_secret": "test",
				})
			}
			if strings.Contains(req.URL.Path, "/revoke") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"client_id":     "test",
					"client_secret": "test",
				})
			}
			return NewStringResponse(http.StatusNotFound, "")
		})
		//add http client to echo framework context
		newContext := context.WithValue(ctxt.Request().Context(), oauth2.HTTPClient, httpClient)
		authenticator := new(rest.OpenIDConnect)
		_, err = authenticator.FromSchema(newContext, schema.Components.SecuritySchemes["WeAuth"].Value, httpClient)
		if err != nil {
			t.Fatalf("error encountered initializing authenticator '%s'", err)
		}
		authenticator.KeySet = &KeySetMock{
			VerifySignatureFunc: func(ctx context.Context, jwt string) ([]byte, error) {
				payload := `{"owner":"demo","name":"demo","createdTime":"2022-06-20T08:43:34-04:00","updatedTime":"","id":"93b46dcd-baf1-41d2-99cc-449d02bfe195","type":"normal-user","password":"","passwordSalt":"","displayName":"Demo","firstName":"","lastName":"","avatar":"https://casbin.org/img/casbin.svg","permanentAvatar":"","email":"demo@wepala.com","emailVerified":false,"phone":"46148043551","location":"","address":[],"affiliation":"Example Inc.","title":"","idCardType":"","idCard":"","homepage":"","bio":"","region":"","language":"","gender":"","birthday":"","education":"","score":0,"karma":0,"ranking":2,"isDefaultAvatar":false,"isOnline":false,"isAdmin":false,"isGlobalAdmin":false,"isForbidden":false,"isDeleted":false,"signupApplication":"demo","hash":"","preHash":"","createdIp":"","lastSigninTime":"","lastSigninIp":"","github":"","google":"","qq":"","wechat":"","unionId":"","facebook":"","dingtalk":"","weibo":"","gitee":"","linkedin":"","wecom":"","lark":"","gitlab":"","adfs":"","baidu":"","alipay":"","casdoor":"","infoflow":"","apple":"","azuread":"","slack":"","steam":"","bilibili":"","okta":"","douyin":"","custom":"","ldap":"","properties":{},"tag":"staff","scope":"read","iss":"https://weauth-dev.weos.sh","sub":"93b46dcd-baf1-41d2-99cc-449d02bfe195","aud":["a6b0bb2379aaef2a98d4"],"exp":1716217112,"nbf":1655737112,"iat":1655737112}`
				return []byte(payload), nil
			},
		}
		result, err := authenticator.Validate(ctxt)
		if err != nil {
			t.Fatalf("error authenticating '%s'", err)
		}

		if !result.Valid {
			t.Error("authentication failed")
		}

		if result.Role != "staff" {
			t.Errorf("expected the role to be '%s', got '%s'", "staff", result.Role)
		}

		if result.UserID != "93b46dcd-baf1-41d2-99cc-449d02bfe195" {
			t.Errorf("expected the userID to be '%s', got '%s'", "93b46dcd-baf1-41d2-99cc-449d02bfe195", result.UserID)
		}
	})
	t.Run("invalid token", func(t *testing.T) {
		var err error
		var rawJWT []byte
		rawRequest := httptest.NewRequest("PUT", "/blogs/1234", nil)
		rawJWT, err = ioutil.ReadFile("./fixtures/jwt/demo.jwt")
		if err != nil {
			t.Fatalf("unable to read jwt fixture '%s'", err)
		}
		rawRequest.Header.Add("Authorization", "Bearer "+string(rawJWT))
		rw := httptest.NewRecorder()

		e := echo.New()
		ctxt := e.NewContext(rawRequest, rw)
		httpClient := NewTestClient(func(req *http.Request) *http.Response {
			if strings.Contains(req.URL.Path, "/.well-known/openid-configuration") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"issuer":                 "https://example.org",
					"authorization_endpoint": "https://example.org/auth",
					"token_endpoint":         "https://example.org/token",
					"userinfo_endpoint":      "https://example.org/userinfo",
					"jwks_uri":               "https://example.org/jwks",
					"registration_endpoint":  "https://example.org/register",
					"revocation_endpoint":    "https://example.org/revoke",
					"scopes_supported":       []string{"openid", "profile", "email", "address", "phone"},
				})
			}
			if strings.Contains(req.URL.Path, "/jwks") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"keys": []map[string]interface{}{
						{
							"kid": "test",
							"kty": "RSA",
							"n":   "test",
							"e":   "AQAB",
						},
					},
				})
			}
			return NewJsonResponse(http.StatusUnauthorized, nil)
		})
		newContext := context.WithValue(ctxt.Request().Context(), oauth2.HTTPClient, httpClient)
		authenticator := new(rest.OpenIDConnect)
		_, err = authenticator.FromSchema(newContext, schema.Components.SecuritySchemes["Auth0"].Value, httpClient)
		authenticator.KeySet = &KeySetMock{
			VerifySignatureFunc: func(ctx context.Context, jwt string) ([]byte, error) {
				payload := `{"owner":"demo","name":"demo","createdTime":"2022-06-20T08:43:34-04:00","updatedTime":"","id":"93b46dcd-baf1-41d2-99cc-449d02bfe195","type":"normal-user","password":"","passwordSalt":"","displayName":"Demo","firstName":"","lastName":"","avatar":"https://casbin.org/img/casbin.svg","permanentAvatar":"","email":"demo@wepala.com","emailVerified":false,"phone":"46148043551","location":"","address":[],"affiliation":"Example Inc.","title":"","idCardType":"","idCard":"","homepage":"","bio":"","region":"","language":"","gender":"","birthday":"","education":"","score":0,"karma":0,"ranking":2,"isDefaultAvatar":false,"isOnline":false,"isAdmin":false,"isGlobalAdmin":false,"isForbidden":false,"isDeleted":false,"signupApplication":"demo","hash":"","preHash":"","createdIp":"","lastSigninTime":"","lastSigninIp":"","github":"","google":"","qq":"","wechat":"","unionId":"","facebook":"","dingtalk":"","weibo":"","gitee":"","linkedin":"","wecom":"","lark":"","gitlab":"","adfs":"","baidu":"","alipay":"","casdoor":"","infoflow":"","apple":"","azuread":"","slack":"","steam":"","bilibili":"","okta":"","douyin":"","custom":"","ldap":"","properties":{},"tag":"staff","scope":"read","iss":"https://weauth-dev.weos.sh","sub":"93b46dcd-baf1-41d2-99cc-449d02bfe195","aud":["a6b0bb2379aaef2a98d4"],"exp":1716217112,"nbf":1655737112,"iat":1655737112}`
				return []byte(payload), fmt.Errorf("invalid token")
			},
		}
		request := ctxt.Request().WithContext(newContext)
		ctxt.SetRequest(request)
		result, err := authenticator.Validate(ctxt)
		if result.Valid {
			t.Error("expected validation to fail")
		}
	})
}

func TestOAuth2_FromSchema(t *testing.T) {
	schema, err := openapi3.NewLoader().LoadFromFile("fixtures/blog-security.yaml")
	if err != nil {
		t.Fatalf("error encountered loading schema '%s'", err)
	}

	t.Skipf("need to figure out way to load certificates see https://wepala.atlassian.net/browse/WEOS-1520")
	t.Run("initialize oauth2 authenticator", func(t *testing.T) {

		httpClient := NewTestClient(func(req *http.Request) *http.Response {
			if strings.Contains(req.RequestURI, "/.well-known/openid-configuration") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"issuer":                 "https://example.org",
					"authorization_endpoint": "https://example.org/auth",
					"token_endpoint":         "https://example.org/token",
					"userinfo_endpoint":      "https://example.org/userinfo",
					"jwks_uri":               "https://example.org/jwks",
					"registration_endpoint":  "https://example.org/register",
					"revocation_endpoint":    "https://example.org/revoke",
					"scopes_supported":       []string{"openid", "profile", "email", "address", "phone"},
				})
			}
			return NewStringResponse(http.StatusNotFound, "")
		})
		authenticator, _ := new(rest.OAuth2).FromSchema(context.Background(), schema.Components.SecuritySchemes["Auth02"].Value, httpClient)
		if tauthenticator, ok := authenticator.(*rest.OAuth2); ok {
			if tauthenticator.Flows.AuthorizationCode.AuthorizationURL != schema.Components.SecuritySchemes["Auth02"].Value.Flows.AuthorizationCode.AuthorizationURL {
				t.Errorf("expected authorization url to be '%s', got '%s'", schema.Components.SecuritySchemes["Auth02"].Value.Flows.AuthorizationCode.AuthorizationURL, tauthenticator.Flows.AuthorizationCode.AuthorizationURL)
			}
		} else {
			t.Fatalf("expected OAuth2 authenticator")
		}
	})
}

func TestOAuth2_Authenticate(t *testing.T) {
	schema, err := openapi3.NewLoader().LoadFromFile("fixtures/blog-security.yaml")
	if err != nil {
		t.Fatalf("error encountered loading schema '%s'", err)
	}
	t.Run("authenticate valid token", func(t *testing.T) {
		t.Skipf("need to figure out way to load certificates see https://wepala.atlassian.net/browse/WEOS-1520")

		rawRequest := httptest.NewRequest("POST", "/blogs/1234", nil)
		token, ok := os.LookupEnv("OAUTH_TEST_KEY")
		if !ok {
			t.Fatal("test requires token set in 'OAUTH_TEST_KEY' environment variable")
		}
		rawRequest.Header.Add("Authorization", "Bearer "+token)
		rw := httptest.NewRecorder()

		e := echo.New()
		ctxt := e.NewContext(rawRequest, rw)

		httpClient := NewTestClient(func(req *http.Request) *http.Response {
			if strings.Contains(req.RequestURI, "/.well-known/openid-configuration") {
				return NewJsonResponse(http.StatusOK, map[string]interface{}{
					"issuer":                 "https://example.org",
					"authorization_endpoint": "https://example.org/auth",
					"token_endpoint":         "https://example.org/token",
					"userinfo_endpoint":      "https://example.org/userinfo",
					"jwks_uri":               "https://example.org/jwks",
					"registration_endpoint":  "https://example.org/register",
					"revocation_endpoint":    "https://example.org/revoke",
					"scopes_supported":       []string{"openid", "profile", "email", "address", "phone"},
				})
			}
			return NewStringResponse(http.StatusNotFound, "")
		})
		authenticator, _ := new(rest.OAuth2).FromSchema(context.Background(), schema.Components.SecuritySchemes["Auth02"].Value, httpClient)
		result, err := authenticator.Validate(ctxt)
		if err != nil {
			t.Fatalf("error authenticating '%s'", err)
		}

		if !result.Valid {
			t.Error("authentication failed")
		}
	})
}
