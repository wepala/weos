package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos-service/context"
	"net/textproto"
	"strings"
)

type (
	OperationMiddleware func(*openapi3.Operation, *openapi3.PathItem, *openapi3.Swagger) echo.MiddlewareFunc
	OperationController func(*openapi3.Operation, *openapi3.PathItem, *openapi3.Swagger) echo.HandlerFunc
)

//Context Create go context and add parameter values to context
func Context(operation *openapi3.Operation, path *openapi3.PathItem, spec *openapi3.Swagger) echo.MiddlewareFunc {
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
		switch strings.ToLower(parameter.Value.In) {
		case "header":
			//have to normalize the key name to be able to retrieve from header because of how echo setup up the headers map
			headerName := textproto.CanonicalMIMEHeaderKey(parameter.Value.Name)
			if value, ok := c.Request().Header[headerName]; ok {
				cc = cc.WithValue(cc, parameter.Value.Name, value[0])
			}

		}
	}

	return cc, nil
}
