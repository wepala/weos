package rest

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos-service/context"
	"github.com/wepala/weos-service/model"
	"net/textproto"
	"strings"
)

//Context Create go context and add parameter values to context
func Context(app model.Application, operation *openapi3.Operation, path *openapi3.PathItem, spec *openapi3.Swagger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			var err error
			cc := context.New(c)
			//get account id using the standard header
			accountID := c.Request().Header.Get(context.HeaderXAccountID)
			if accountID != "" {
				cc = cc.WithValue(cc, context.ACCOUNT_ID, accountID)
			}
			//use the path information to get the parameter values and add them to the context
			for _, parameter := range path.Parameters {
				cc, err = parseParams(c, cc, parameter)
			}
			//use the operation information to get the parameter values and add them to the context
			for _, parameter := range operation.Parameters {
				cc, err = parseParams(c, cc, parameter)
			}
			//if there are any errors
			if err != nil {
				c.Logger().Error(err)
			}
			return next(cc)
		}
	}
}

//parseParams uses the parameter type to determine where to pull the value from
func parseParams(c echo.Context, cc *context.Context, parameter *openapi3.ParameterRef) (*context.Context, error) {
	if parameter.Value != nil {
		contextName := parameter.Value.Name
		//if there is a context name specified use that instead. The value is a json.RawMessage (not a string)
		if tcontextName, ok := parameter.Value.ExtensionProps.Extensions[CONTEXT_NAME_EXTENSION]; ok {
			err := json.Unmarshal(tcontextName.(json.RawMessage), &contextName)
			if err != nil {
				return nil, err
			}
		}
		switch strings.ToLower(parameter.Value.In) {
		case "header":
			//have to normalize the key name to be able to retrieve from header because of how echo setup up the headers map
			headerName := textproto.CanonicalMIMEHeaderKey(parameter.Value.Name)
			if value, ok := c.Request().Header[headerName]; ok {
				cc = cc.WithValue(cc, contextName, value[0])
			}
		case "query":
			cc = cc.WithValue(cc, contextName, c.QueryParam(parameter.Value.Name))
		case "path":
			cc = cc.WithValue(cc, contextName, c.Param(parameter.Value.Name))
		}
	}
	//TODO account for $ref tag reference
	return cc, nil
}
