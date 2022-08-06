package rest_test

import (
	"github.com/casbin/casbin/v2"
	casbinmodel "github.com/casbin/casbin/v2/model"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	context2 "github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSecurityConfiguration_FromSchema(t *testing.T) {
	t.Run("set open id authenticator", func(t *testing.T) {
		swagger, err := LoadConfig(t, "fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading config '%s'", err)
		}
		config, err := new(rest.SecurityConfiguration).FromSchema(swagger.Components.SecuritySchemes)
		if err != nil {
			t.Fatalf("unexpected error setting up security configuration '%s'", err)
		}
		if len(config.Validators) != 2 {
			t.Errorf("expected %d authenticators to be setup, got %d", 2, len(config.Validators))
		}
	})
	t.Run("set unrecognized authenticator", func(t *testing.T) {
		swagger, err := LoadConfig(t, "fixtures/blog-security-invalid.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading config '%s'", err)
		}
		config, err := new(rest.SecurityConfiguration).FromSchema(swagger.Components.SecuritySchemes)
		if err == nil {
			t.Error("unexpected error for invalid securityScheme type")
		}
		if len(config.Validators) > 0 {
			t.Errorf("expected %d authenticators to be setup, got %d", 0, len(config.Validators))
		}
	})
}

func TestSecurityConfiguration_Middleware(t *testing.T) {
	t.Run("valid authenticator", func(t *testing.T) {
		swagger, err := LoadConfig(t, "fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading config '%s'", err)
		}
		config, err := new(rest.SecurityConfiguration).FromSchema(swagger.Components.SecuritySchemes)
		if err != nil {
			t.Fatalf("unexpected error setting up security configuration '%s'", err)
		}
		//set mock authenticator, so we can check that it was set and called
		mockAuthenticator := &ValidatorMock{ValidateFunc: func(ctxt echo.Context) (bool, interface{}, string, string, string, string, error) {
			return true, nil, "", "", "", "", nil
		}}
		config.Validators["Auth0"] = mockAuthenticator
		//find path with no security scheme set
		path := swagger.Paths.Find("/blogs")
		container := &ContainerMock{
			GetSecurityConfigurationFunc: func() *rest.SecurityConfiguration {
				return config
			},
			GetLogFunc: func(name string) (model.Log, error) {
				return &LogMock{
					DebugfFunc: func(format string, args ...interface{}) {

					},
					ErrorfFunc: func(format string, args ...interface{}) {

					},
				}, nil
			},
			GetConfigFunc: func() *openapi3.Swagger {
				return swagger
			},
			GetPermissionEnforcerFunc: func(name string) (*casbin.Enforcer, error) {
				//default REST permission model
				text := `[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && keyMatch(r.obj, p.obj) && regexMatch(r.act, p.act)
`
				m, _ := casbinmodel.NewModelFromString(text)
				return casbin.NewEnforcer(m, "./fixtures/permissions.csv")
			},
		}
		mw := config.Middleware(container, &CommandDispatcherMock{}, &EntityRepositoryMock{}, path, path.Post)
		nextMiddlewareHit := false
		handler := mw(func(context echo.Context) error {
			nextMiddlewareHit = true
			return nil
		})

		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", nil)
		e.POST("/blogs", handler)
		e.ServeHTTP(resp, req)

		if len(mockAuthenticator.ValidateCalls()) != 1 {
			t.Errorf("expected the mock authenticator to be called %d time, called %d times", 1, len(mockAuthenticator.ValidateCalls()))
		}

		if !nextMiddlewareHit {
			t.Errorf("expected the next middleware to be hit")
		}
	})
	t.Run("invalid scopes", func(t *testing.T) {
		api, err := rest.New("./fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api config '%s'", err)
		}
		swagger := api.Swagger
		config, err := new(rest.SecurityConfiguration).FromSchema(swagger.Components.SecuritySchemes)
		if err != nil {
			t.Fatalf("unexpected error setting up security configuration '%s'", err)
		}
		api.RegisterSecurityConfiguration(config)
		path := swagger.Paths.Find("/blogs/{id}")
		mw := config.Middleware(api, &CommandDispatcherMock{}, &EntityRepositoryMock{}, path, path.Get)
		handler := mw(func(context echo.Context) error {
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/blogs/1234", nil)
		token := os.Getenv("OAUTH_TEST_KEY")
		req.Header.Add("Authorization", "Bearer "+token)
		e.GET("/blogs/1234", handler)
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusForbidden {
			t.Errorf("expected the response to be %d, got %d", http.StatusForbidden, resp.Result().StatusCode)
		}
	})

	t.Run("confirm claims in context", func(t *testing.T) {
		api, err := rest.New("./fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api config '%s'", err)
		}
		swagger := api.Swagger
		err = api.Initialize(context.TODO())
		if err != nil {
			t.Fatalf("error initializing api")
		}
		config := api.GetSecurityConfiguration()
		path := swagger.Paths.Find("/blogs")
		mw := config.Middleware(api, &CommandDispatcherMock{}, &EntityRepositoryMock{}, path, path.Get)
		handler := mw(func(ctxt echo.Context) error {
			//check that the there is a role in the context
			if context2.GetUser(ctxt.Request().Context()) != "auth0|60d0c84316f69600691c1614" {
				t.Errorf("expected user to be '%s', got %s", "auth0|60d0c84316f69600691c1614", context2.GetUser(ctxt.Request().Context()))
			}
			if context2.GetAccount(ctxt.Request().Context()) != "auth0|60d0c84316f69600691c1614" {
				t.Errorf("expected account to be '%s', got %s", "auth0|60d0c84316f69600691c1614", context2.GetAccount(ctxt.Request().Context()))
			}
			if context2.GetApplication(ctxt.Request().Context()) != "https://dev-bhjqt6zc.us.auth0.com/" {
				t.Errorf("expected application to be '%s', got %s", "https://dev-bhjqt6zc.us.auth0.com/", context2.GetApplication(ctxt.Request().Context()))
			}
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", nil)
		token := os.Getenv("OAUTH_TEST_KEY")
		req.Header.Add("Authorization", "Bearer "+token)
		e.POST("/blogs", handler)
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusOK {
			t.Errorf("expected the response to be %d, got %d", http.StatusOK, resp.Result().StatusCode)
		}
	})
	t.Run("allowed user access", func(t *testing.T) {
		api, err := rest.New("./fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api config '%s'", err)
		}
		swagger := api.Swagger
		err = api.Initialize(context.TODO())
		if err != nil {
			t.Fatalf("error initializing api")
		}
		config := api.GetSecurityConfiguration()
		path := swagger.Paths.Find("/blogs")
		mw := config.Middleware(api, &CommandDispatcherMock{}, &EntityRepositoryMock{}, path, path.Get)
		handler := mw(func(context echo.Context) error {
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", nil)
		token := os.Getenv("OAUTH_TEST_KEY")
		req.Header.Add("Authorization", "Bearer "+token)
		e.POST("/blogs", handler)
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusOK {
			t.Errorf("expected the response to be %d, got %d", http.StatusOK, resp.Result().StatusCode)
		}
	})
	t.Run("allowed role access", func(t *testing.T) {
		api, err := rest.New("./fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api config '%s'", err)
		}
		swagger := api.Swagger
		err = api.Initialize(context.TODO())
		if err != nil {
			t.Fatalf("error initializing api")
		}
		config := api.GetSecurityConfiguration()
		path := swagger.Paths.Find("/blogs")
		mw := config.Middleware(api, &CommandDispatcherMock{}, &EntityRepositoryMock{}, path, path.Get)
		handler := mw(func(context echo.Context) error {
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/blogs", nil)
		token := os.Getenv("OAUTH_TEST_KEY")
		req.Header.Add("Authorization", "Bearer "+token)
		e.POST("/blogs", handler)
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusOK {
			t.Errorf("expected the response to be %d, got %d", http.StatusOK, resp.Result().StatusCode)
		}
	})

	t.Run("unauthorized access", func(t *testing.T) {
		api, err := rest.New("./fixtures/blog-security.yaml")
		if err != nil {
			t.Fatalf("unexpected error loading api config '%s'", err)
		}
		swagger := api.Swagger
		err = api.Initialize(context.TODO())
		if err != nil {
			t.Fatalf("error initializing api")
		}
		config := api.GetSecurityConfiguration()
		path := swagger.Paths.Find("/authors/{id}")
		mw := config.Middleware(api, &CommandDispatcherMock{}, &EntityRepositoryMock{}, path, path.Get)
		handler := mw(func(context echo.Context) error {
			return nil
		})
		e := echo.New()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/authors/1234", nil)
		token := os.Getenv("OAUTH_TEST_KEY")
		req.Header.Add("Authorization", "Bearer "+token)
		e.GET("/authors/1234", handler)
		e.ServeHTTP(resp, req)

		if resp.Result().StatusCode != http.StatusForbidden {
			t.Errorf("expected the response to be %d, got %d", http.StatusForbidden, resp.Result().StatusCode)
		}
	})
}

//func TestSecurityConfiguration_SetDefaultSecurity(t *testing.T) {
//	swagger, err := LoadConfig(t, "fixtures/blog-security.yaml")
//	if err != nil {
//		t.Fatalf("unexpected error loading config '%s'", err)
//	}
//	config, err := new(rest.SecurityConfiguration).FromSchema(swagger.Components.SecuritySchemes)
//	config.SetDefaultSecurity(swagger.Security.With())
//}
