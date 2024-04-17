package rest

import (
	"github.com/casbin/casbin/v2"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/segmentio/ksuid"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

type MiddlewareParams struct {
	Logger                Log
	CommandDispatcher     CommandDispatcher
	ResourceRepository    *ResourceRepository
	Schema                *openapi3.T
	APIConfig             *APIConfig
	PathMap               map[string]*openapi3.PathItem
	Operation             map[string]*openapi3.Operation
	SecuritySchemes       map[string]Validator
	AuthorizationEnforcer *casbin.Enforcer
}

// ZapLogger switches the echo context logger to be ZapLogger
func ZapLogger(p *MiddlewareParams) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		var configuredLevel string
		var serviceName string
		if p.APIConfig.Log != nil {
			configuredLevel = p.APIConfig.Log.Level
			serviceName = p.APIConfig.Log.Name
		}
		return func(c echo.Context) error {
			//setting the default logger in the context as zap with the default mode being error
			req := c.Request()
			id := req.Header.Get(echo.HeaderXRequestID)
			if id == "" {
				id = ksuid.New().String()
				req.Header.Set(echo.HeaderXRequestID, id)
			}
			level := req.Header.Get("X-Log-Level")
			if level == "" {
				if configuredLevel != "" {
					level = configuredLevel
				} else { //by default only show errors
					level = "info"
				}
			} else {
				//only allow setting the level to debug from this header for security reasons
				level = "debug"
			}
			if serviceName == "" {
				serviceName = "weos"
			}
			zapLogger, err := new(Zap).WithRequestID(serviceName, level, id)
			if err != nil {
				c.Logger().Errorf("Unexpected error setting the context logger : %s", err)
			}
			c.SetLogger(zapLogger)
			start := time.Now()
			cc := c.Request().Context()
			cc = context.WithValue(cc, echo.HeaderXRequestID, id)
			request := c.Request().WithContext(cc)
			c.SetRequest(request)
			err = next(c)
			response := c.Response()
			re := regexp.MustCompile(`^` + os.Getenv("BASE_PATH") + `/health`)
			if !re.MatchString(request.URL.Path) {
				zapLogger.With(
					zap.String("remote_ip", c.RealIP()),
					zap.String("uri", req.RequestURI),
					zap.Int("status", response.Status),
					zap.String("method", c.Request().Method),
					zap.Duration("latency", time.Since(start)),
					zap.Int64("response_size", response.Size),
					zap.String("referer", req.Referer()),
					zap.String("user_agent", req.UserAgent()),
				).Info("request")
			}
			return err
		}
	}
}

func SecurityMiddleware(p *MiddlewareParams) echo.MiddlewareFunc {
	//check that the schemes exist
	var validators []Validator
	logger := p.Logger
	securitySchemes := p.Schema.Security
	for _, operation := range p.Operation {
		if operation.Security != nil {
			if len(*operation.Security) > 0 {
				securitySchemes = append(securitySchemes, *operation.Security...)
			} else { //if an empty array is set for the security scheme then no security scheme should be set
				securitySchemes = make(openapi3.SecurityRequirements, 0)
			}
		}
	}

	for _, scheme := range securitySchemes {
		for name, _ := range scheme {
			allValidators := p.SecuritySchemes
			if validator, ok := allValidators[name]; ok {
				validators = append(validators, validator)
			} else {
				logger.Errorf("security scheme '%s' was not configured in components > security schemes", name)
			}
		}

	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctxt echo.Context) error {
			var success bool
			var err error
			var result *ValidationResult
			//loop through the validators and go to the next middleware when one authenticates otherwise return 403
			for _, validator := range validators {
				if result, err = validator.Validate(ctxt); result.Valid {
					newContext := context.WithValue(ctxt.Request().Context(), USER_ID, result.UserID)
					newContext = context.WithValue(newContext, ROLE, result.Role)
					newContext = context.WithValue(newContext, ACCOUNT_ID, result.AccountID)
					newContext = context.WithValue(newContext, APPLICATION_ID, result.ApplicationID)
					newContext = context.WithValue(newContext, AUTHORIZATION_HEADER, ctxt.Request().Header.Get("Authorization"))

					request := ctxt.Request().WithContext(newContext)
					ctxt.SetRequest(request)
					//check the scopes of the logged-in user against what is required and if the user doesn't have the required scope deny access
					for _, securityScheme := range securitySchemes {
						for _, scopes := range securityScheme {
							for _, scope := range scopes {
								//account for the different token types that could be returned
								switch t := result.Token.(type) {
								case *oidc.IDToken:
									claims := make(map[string]interface{})
									err = t.Claims(&claims)
									if err != nil {
										ctxt.Logger().Debugf("invalid claims '%s'", err)
									}
									if _, ok := claims["scope"]; !ok {
										ctxt.Logger().Debug("token from issuer '%s' does not have scopes", t.Issuer)
										return ctxt.NoContent(http.StatusForbidden)
									}
									//if the required scope is not in the user scope
									if !strings.Contains(claims["scope"].(string), scope) {
										ctxt.Logger().Debug("token from issuer '%s' does not have required scope '%s'", t.Issuer, scope)
										return ctxt.NoContent(http.StatusForbidden)
									}
								case *jwt.Token:
									if claims, ok := t.Claims.(jwt.MapClaims); ok {
										if _, ok := claims["scope"]; !ok {
											ctxt.Logger().Debug("token from issuer '%s' does not have scopes", claims["iss"])
											return ctxt.NoContent(http.StatusForbidden)
										}
										//if the required scope is not in the user scope
										if !strings.Contains(claims["scope"].(string), scope) {
											ctxt.Logger().Debug("token from issuer '%s' does not have required scope '%s'", claims["iss"], scope)
											return ctxt.NoContent(http.StatusForbidden)
										}
									}
								}
							}
						}
					}
					//check permissions to ensure the user can access this endpoint

					tpath := strings.Replace(ctxt.Request().URL.Path, p.APIConfig.BasePath, "", 1)
					success, err = p.AuthorizationEnforcer.Enforce(result.UserID, tpath, ctxt.Request().Method)
					//fmt.Printf("explanations %v", explanations)
					if err != nil {
						ctxt.Logger().Errorf("error looking up permissions '%s'", err)
					}
					if success {
						return next(ctxt)
					}
					//check if the role has access to the endpoint
					success, err = p.AuthorizationEnforcer.Enforce(result.Role, tpath, ctxt.Request().Method)
					if success {
						return next(ctxt)
					}
					if err != nil {
						ctxt.Logger().Errorf("the role '%s' does not have access to '%s' action '%s': original error '%s'", result.Role, tpath, ctxt.Request().Method, err)
					}
					return ctxt.NoContent(http.StatusForbidden)
				}
				ctxt.Logger().Debugf("error authenticating '%s'", err)
			}
			//if there were validators configured then return un authorized status code
			if len(validators) > 0 {
				return ctxt.NoContent(http.StatusUnauthorized)
			}

			return next(ctxt)
		}
	}
}
