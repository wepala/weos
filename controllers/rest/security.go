package rest

import (
	"context"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	context2 "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"net/http"
)

//Validator interface that must be implemented so that a request can be authenticated
type Validator interface {
	//Validate validate and return token, user, role
	Validate(ctxt echo.Context) (bool, interface{}, string, string, error)
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

func (s *SecurityConfiguration) Middleware(api Container, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
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
			//loop through the validators and go to the next middleware when one authenticates otherwise return 403
			for _, validator := range validators {
				var success bool
				var err error
				var userID string
				if success, _, userID, _, err = validator.Validate(ctxt); success {
					newContext := context.WithValue(ctxt.Request().Context(), context2.USER_ID, userID)
					request := ctxt.Request().WithContext(newContext)
					ctxt.SetRequest(request)
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
