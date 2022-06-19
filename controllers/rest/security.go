package rest

import (
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"net/http"
)

//Authenticator interface that must be implemented so that a request can be authenticated
type Authenticator interface {
	Authenticate(ctxt echo.Context) (bool, error)
	FromSchema(scheme *openapi3.SecurityScheme) (Authenticator, error)
}

//SecurityConfiguration mange the security configuration for the API
type SecurityConfiguration struct {
	schemas        []*openapi3.SchemaRef
	defaultConfig  []map[string][]string
	Authenticators map[string]Authenticator
}

func (s *SecurityConfiguration) FromSchema(schemas map[string]*openapi3.SecuritySchemeRef) (*SecurityConfiguration, error) {
	var err error
	//configure the authenticators based on the schemas
	s.Authenticators = make(map[string]Authenticator)
	for name, schema := range schemas {
		if schema.Value != nil {
			switch schema.Value.Type {
			case "openIdConnect":
				s.Authenticators[name], err = new(OpenIDConnect).FromSchema(schema.Value)
			default:
				err = fmt.Errorf("unsupported security scheme '%s'", name)
				return s, err
			}
		}
	}
	return s, err
}

func (s *SecurityConfiguration) SetDefaultSecurity(config []map[string][]string) {
	s.defaultConfig = config
}

func (s *SecurityConfiguration) Middleware(api Container, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	//check that the schemes exist
	var authenticators []Authenticator
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
			allAuthenticators := api.GetSecurityConfiguration().Authenticators
			if authenticator, ok := allAuthenticators[name]; ok {
				authenticators = append(authenticators, authenticator)
			} else {
				logger.Errorf("security scheme '%s' was not configured in components > security schemes", name)
			}
		}

	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctxt echo.Context) error {
			//loop through the authenticators and go to the next middleware when one authenticates otherwise return 403
			for _, authenticator := range authenticators {
				var success bool
				var err error
				if success, err = authenticator.Authenticate(ctxt); success {
					return next(ctxt)
				} else {
					ctxt.Logger().Debugf("error authenticating '%s'", err)
				}
			}
			return ctxt.NoContent(http.StatusForbidden)
		}
	}
}

//Authenticators

type OpenIDConnect struct {
	connectURL string
}

func (o OpenIDConnect) Authenticate(ctxt echo.Context) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (o OpenIDConnect) FromSchema(scheme *openapi3.SecurityScheme) (Authenticator, error) {
	var err error
	if tinterface, ok := scheme.Extensions[OpenIDConnectUrlExtension]; ok {
		if rawURL, ok := tinterface.(json.RawMessage); ok {
			err = json.Unmarshal(rawURL, &o.connectURL)
		}
	}
	return o, err
}
