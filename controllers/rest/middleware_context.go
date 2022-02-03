package rest

import (
	"encoding/json"
	"fmt"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	weosContext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
)

//Context CreateHandler go context and add parameter values to context
func Context(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			var err error
			cc := c.Request().Context()
			//get account id using the standard header
			accountID := c.Request().Header.Get(weosContext.HeaderXAccountID)
			if accountID != "" {
				cc = context.WithValue(cc, weosContext.ACCOUNT_ID, accountID)
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
			request := c.Request().WithContext(cc)
			c.SetRequest(request)
			return next(c)
		}
	}
}

//parseParams uses the parameter type to determine where to pull the value from
func parseParams(c echo.Context, cc context.Context, parameter *openapi3.ParameterRef) (context.Context, error) {
	if parameter.Value != nil {
		contextName := parameter.Value.Name
		paramType := parameter.Value.Schema
		//if there is a context name specified use that instead. The value is a json.RawMessage (not a string)
		if tcontextName, ok := parameter.Value.ExtensionProps.Extensions[ContextNameExtension]; ok {
			err := json.Unmarshal(tcontextName.(json.RawMessage), &contextName)
			if err != nil {
				return nil, err
			}
		}
		//if there is an alias name specified use that instead. The value is a json.RawMessage (not a string)
		if tcontextName, ok := parameter.Value.ExtensionProps.Extensions[AliasExtension]; ok {
			err := json.Unmarshal(tcontextName.(json.RawMessage), &contextName)
			if err != nil {
				return nil, err
			}
		}
		var val interface{}
		switch strings.ToLower(parameter.Value.In) {
		//parameter values stored as strings
		case "header":
			//have to normalize the key name to be able to retrieve from header because of how echo setup up the headers map
			headerName := textproto.CanonicalMIMEHeaderKey(parameter.Value.Name)
			if value, ok := c.Request().Header[headerName]; ok {
				val = value[0]
			}
		case "query":
			val = c.QueryParam(parameter.Value.Name)
		case "path":
			val = c.Param(parameter.Value.Name)
		}

		if _, ok := val.(string); ok {
			switch parameter.Value.Name {
			case "sequence_no": //default type is integer
				v, err := strconv.Atoi(val.(string))
				if err == nil {
					val = v
				}
			case "use_entity_id": //default type is boolean
				v, err := strconv.ParseBool(val.(string))
				if err == nil {
					val = v
				}
			case "If-Match", "If-None-Match": //default type is string
			default:
				var filters map[string]*FilterProperties
				filters = map[string]*FilterProperties{}
				if parameter.Value.Name == "_filters" {
					filtersArray := SplitFilters(c.Request().URL.RawQuery)
					if filtersArray != nil && len(filtersArray) > 0 {
						for _, value := range filtersArray {
							prop := SplitFilter(value)
							if prop == nil {
								return cc, fmt.Errorf("unexpected error filter format is incorrect")
							}
							filters[prop.Field] = prop
						}
						val = filters
						break
					}

				}
				if paramType != nil && paramType.Value != nil {
					pType := paramType.Value.Type
					switch strings.ToLower(pType) {
					case "integer":
						v, err := strconv.Atoi(val.(string))
						if err == nil {
							val = v
						}
					case "boolean":
						v, err := strconv.ParseBool(val.(string))
						if err == nil {
							val = v
						}
					case "number":
						format := paramType.Value.Format
						if format == "float" || format == "double" {
							v, err := strconv.ParseFloat(val.(string), 64)
							if err == nil {
								val = v
							}
						} else {
							v, err := strconv.Atoi(val.(string))
							if err == nil {
								val = v
							}
						}

					}
				}
			}
		}
		cc = context.WithValue(cc, contextName, val)
	}
	//TODO account for $ref tag reference
	return cc, nil
}
