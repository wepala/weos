package rest

import (
	"context"
	"fmt"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	context2 "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"net/http"
	"strings"
)

//Validator interface that must be implemented so that a request can be authenticated
type Validator interface {
	//Validate validate and return token, user, role
	Validate(ctxt echo.Context) (bool, interface{}, string, string, string, string, error)
	FromSchema(scheme *openapi3.SecurityScheme) (Validator, error)
}

//SecurityConfiguration mange the security configuration for the API
type SecurityConfiguration struct {
	schemas       []*openapi3.SchemaRef
	defaultConfig []map[string][]string
	Validators    map[string]Validator
}

func (s *SecurityConfiguration) FromSchema(schemas map[string]*openapi3.SecuritySchemeRef) (*SecurityConfiguration, error) {
	var err error
	//configure the authenticators based on the schemas
	s.Validators = make(map[string]Validator)
	for name, schema := range schemas {
		if schema.Value != nil {
			switch schema.Value.Type {
			case "openIdConnect":
				s.Validators[name], err = new(OpenIDConnect).FromSchema(schema.Value)
			default:
				err = fmt.Errorf("unsupported security scheme '%s'", name)
				return s, err
			}
		}
	}
	return s, err
}

func (s *SecurityConfiguration) Middleware(api Container, commandDispatcher model.CommandDispatcher, repository model.EntityRepository, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	//check that the schemes exist
	var validators []Validator
	logger, _ := api.GetLog("Default")
	if logger == nil {
		logger = log.New("log")
	}
	securitySchemes := api.GetConfig().Security
	if operation.Security != nil {
		if len(*operation.Security) > 0 {
			securitySchemes = append(securitySchemes, *operation.Security...)
		} else { //if an empty array is set for the security scheme then no security scheme should be set
			securitySchemes = make(openapi3.SecurityRequirements, 0)
		}
	}

	for _, scheme := range securitySchemes {
		for name, _ := range scheme {
			allValidators := api.GetSecurityConfiguration().Validators
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
			var userID string
			var ttoken interface{} //parsed token
			var role string
			var accountID string
			var applicationID string
			//loop through the validators and go to the next middleware when one authenticates otherwise return 403
			for _, validator := range validators {
				if success, ttoken, userID, role, accountID, applicationID, err = validator.Validate(ctxt); success {
					newContext := context.WithValue(ctxt.Request().Context(), context2.USER_ID, userID)
					newContext = context.WithValue(newContext, context2.ROLE, role)
					newContext = context.WithValue(newContext, context2.ACCOUNT_ID, accountID)
					newContext = context.WithValue(newContext, context2.APPLICATION_ID, applicationID)
					request := ctxt.Request().WithContext(newContext)
					ctxt.SetRequest(request)
					//check the scopes of the logged-in user against what is required and if the user doesn't have the required scope deny access
					for _, securityScheme := range securitySchemes {
						for _, scopes := range securityScheme {
							for _, scope := range scopes {
								//account for the different token types that could be returned
								switch t := ttoken.(type) {
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
					if enforcer, err := api.GetPermissionEnforcer("Default"); err == nil {
						tpath := strings.Replace(ctxt.Request().URL.Path, api.GetWeOSConfig().BasePath, "", 1)
						success, err = enforcer.Enforce(userID, tpath, ctxt.Request().Method)
						//fmt.Printf("explanations %v", explanations)
						if err != nil {
							ctxt.Logger().Errorf("error looking up permissions '%s'", err)
						}
						if success {
							return next(ctxt)
						}
						//check if the role has access to the endpoint
						success, err = enforcer.Enforce(role, tpath, ctxt.Request().Method)
						if success {
							return next(ctxt)
						}
						if err != nil {
							ctxt.Logger().Errorf("the role '%s' does not have access to '%s' action '%s': original error", role, tpath, ctxt.Request().Method, err)
						}
						ctxt.Logger().Errorf("the role '%s' does not have access to '%s' action '%s': original error", role, tpath, ctxt.Request().Method, err)
						return ctxt.NoContent(http.StatusForbidden)
					}
					return next(ctxt)
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
