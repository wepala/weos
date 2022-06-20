package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"net/http"
	"net/http/httptest"
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
		if len(config.Validators) != 1 {
			t.Errorf("expected %d authenticators to be setup, got %d", 1, len(config.Validators))
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
		//set mock authenticator so we can check that is was set and called
		mockAuthenticator := &AuthenticatorMock{AuthenticateFunc: func(ctxt echo.Context) (bool, error) {
			return true, nil
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
		}
		mw := config.Middleware(container, &ProjectionMock{}, &CommandDispatcherMock{}, &EventRepositoryMock{}, &EntityFactoryMock{}, path, path.Post)
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

		if len(mockAuthenticator.AuthenticateCalls()) != 1 {
			t.Errorf("expected the mock authenticator to be called %d time, called %d times", 1, len(mockAuthenticator.AuthenticateCalls()))
		}

		if !nextMiddlewareHit {
			t.Errorf("expected the next middleware to be hit")
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
