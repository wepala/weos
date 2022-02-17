package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"io/ioutil"
	"net/http"
	"net/textproto"
	"net/url"
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
				cc, err = parseParams(c, cc, parameter, entityFactory)
			}
			//use the operation information to get the parameter values and add them to the context
			for _, parameter := range operation.Parameters {
				cc, err = parseParams(c, cc, parameter, entityFactory)
			}

			//parse request body based on content type
			var payload []byte
			ct := c.Request().Header.Get("Content-Type")
			//split the content type on ; and use the first segment. This is based on multidata
			ctParts := strings.Split(ct, ";")
			ct = ctParts[0]

			//check if content type was defined in the schema
			if operation.RequestBody != nil && operation.RequestBody.Value != nil {
				mimeType := operation.RequestBody.Value.Content.Get(ct)
				if mimeType != nil {
					switch ct {
					case "application/json":
						payload, err = ioutil.ReadAll(c.Request().Body)
					default:
						payload, err = ConvertFormToJson(c.Request(), ct)
					}
					//set payload to context
					cc = context.WithValue(cc, weosContext.PAYLOAD, payload)
				} else {
					c.Logger().Debugf("content-type '%s' not supported", ct)
					return NewControllerError(fmt.Sprintf("the content-type '%s' is not supported", ct), err, http.StatusBadRequest)
				}
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
func parseParams(c echo.Context, cc context.Context, parameter *openapi3.ParameterRef, entityFactory model.EntityFactory) (context.Context, error) {
	if entityFactory == nil {
		c.Logger().Error("no entity factory found")
		return cc, fmt.Errorf("no entity factory found ")
	}
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
			case "If-Match", "If-None-Match", "Authorization": //default type is string
			default:
				var filters map[string]interface{}
				filters = map[string]interface{}{}
				if parameter.Value.Name == "_filters" {
					decodedQuery, err := url.PathUnescape(c.Request().URL.RawQuery)
					if err != nil {
						return cc, fmt.Errorf("Error decoding the string %v", err)
					}
					filtersArray := SplitFilters(decodedQuery)
					if filtersArray != nil && len(filtersArray) > 0 {
						for _, value := range filtersArray {
							if strings.Contains(value, "_filters") {
								prop := SplitFilter(value)
								if prop == nil {
									return cc, fmt.Errorf("unexpected error filter format is incorrect: %s", value)
								}
								filters[prop.Field] = prop
							}
						}
						filters, err = convertProperties(filters, entityFactory.Schema())
						if err != nil {
							return cc, err
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

//convertProperties is used to convert the filter value to the correct data type based on the schema
func convertProperties(properties map[string]interface{}, schema *openapi3.Schema) (map[string]interface{}, error) {
	if properties == nil {
		return nil, errors.New("no filters found")
	}

	for field, filterProperty := range properties {

		if field == "id" && schema.Properties[field] == nil {
			//Checks if the filter value field is not empty
			if filterProperty.(*FilterProperties).Value != nil && filterProperty.(*FilterProperties).Value.(string) != "" {
				v, err := strconv.ParseUint(filterProperty.(*FilterProperties).Value.(string), 10, 32)
				if err != nil {
					msg := "unexpected error converting filter field " + field + "to correct data type"
					return nil, errors.New(msg)
				}
				properties[field] = &FilterProperties{
					Field:    field,
					Operator: filterProperty.(*FilterProperties).Operator,
					Value:    v,
				}
				break
			}
			//checks if the length of filter values is not 0
			if len(filterProperty.(*FilterProperties).Values) != 0 {
				arr := []interface{}{}
				for _, val := range filterProperty.(*FilterProperties).Values {
					v, err := strconv.ParseUint(val.(string), 10, 32)
					if err != nil {
						msg := "unexpected error converting filter field " + field + "to correct data type"
						return nil, errors.New(msg)
					}
					arr = append(arr, v)
				}

				properties[field] = &FilterProperties{
					Field:    field,
					Operator: filterProperty.(*FilterProperties).Operator,
					Values:   arr,
				}
				break
			}
		}
		if schema.Properties[field] != nil {
			switch schema.Properties[field].Value.Type {
			case "integer":
				//Checks if the filter value field is not empty
				if filterProperty.(*FilterProperties).Value != nil && filterProperty.(*FilterProperties).Value.(string) != "" {
					v, err := strconv.Atoi(filterProperty.(*FilterProperties).Value.(string))
					if err != nil {
						msg := "unexpected error converting filter field " + field + "to correct data type"
						return nil, errors.New(msg)
					}
					properties[field] = &FilterProperties{
						Field:    field,
						Operator: filterProperty.(*FilterProperties).Operator,
						Value:    v,
					}
					break
				}
				//checks if the length of filter values is not 0
				if len(filterProperty.(*FilterProperties).Values) != 0 {
					arr := []interface{}{}
					for _, val := range filterProperty.(*FilterProperties).Values {
						v, err := strconv.Atoi(val.(string))
						if err != nil {
							msg := "unexpected error converting filter field " + field + "to correct data type"
							return nil, errors.New(msg)
						}
						arr = append(arr, v)
					}

					properties[field] = &FilterProperties{
						Field:    field,
						Operator: filterProperty.(*FilterProperties).Operator,
						Values:   arr,
					}
					break
				}

			case "boolean":
				if filterProperty.(*FilterProperties).Value != nil && filterProperty.(*FilterProperties).Value.(string) != "" {
					v, err := strconv.ParseBool(filterProperty.(*FilterProperties).Value.(string))
					if err != nil {
						msg := "unexpected error converting filter field " + field + "to correct data type"
						return nil, errors.New(msg)
					}
					properties[field] = &FilterProperties{
						Field:    field,
						Operator: filterProperty.(*FilterProperties).Operator,
						Value:    v,
					}
					break
				}
				if len(filterProperty.(*FilterProperties).Values) != 0 {
					arr := []interface{}{}
					for _, val := range filterProperty.(*FilterProperties).Values {
						v, err := strconv.ParseBool(val.(string))
						if err != nil {
							msg := "unexpected error converting filter field " + field + "to correct data type"
							return nil, errors.New(msg)
						}
						arr = append(arr, v)
					}

					properties[field] = &FilterProperties{
						Field:    field,
						Operator: filterProperty.(*FilterProperties).Operator,
						Values:   arr,
					}
					break
				}

			case "number":
				format := schema.Properties[field].Value.Format
				if format == "float" || format == "double" {
					if filterProperty.(*FilterProperties).Value != nil && filterProperty.(*FilterProperties).Value.(string) != "" {
						v, err := strconv.ParseFloat(filterProperty.(*FilterProperties).Value.(string), 64)
						if err != nil {
							msg := "unexpected error converting filter field " + field + "to correct data type"
							return nil, errors.New(msg)
						}
						properties[field] = &FilterProperties{
							Field:    field,
							Operator: filterProperty.(*FilterProperties).Operator,
							Value:    v,
						}
						break
					}
					if len(filterProperty.(*FilterProperties).Values) != 0 {
						arr := []interface{}{}
						for _, val := range filterProperty.(*FilterProperties).Values {
							v, err := strconv.ParseFloat(val.(string), 64)
							if err != nil {
								msg := "unexpected error converting filter field " + field + "to correct data type"
								return nil, errors.New(msg)
							}
							arr = append(arr, v)
						}

						properties[field] = &FilterProperties{
							Field:    field,
							Operator: filterProperty.(*FilterProperties).Operator,
							Values:   arr,
						}
						break
					}
				} else {
					if filterProperty.(*FilterProperties).Value != nil && filterProperty.(*FilterProperties).Value.(string) != "" {
						v, err := strconv.Atoi(filterProperty.(*FilterProperties).Value.(string))
						if err != nil {
							msg := "unexpected error converting filter field " + field + "to correct data type"
							return nil, errors.New(msg)
						}
						properties[field] = &FilterProperties{
							Field:    field,
							Operator: filterProperty.(*FilterProperties).Operator,
							Value:    v,
						}
						break
					}
					if len(filterProperty.(*FilterProperties).Values) != 0 {
						arr := []interface{}{}
						for _, val := range filterProperty.(*FilterProperties).Values {
							v, err := strconv.Atoi(val.(string))
							if err != nil {
								msg := "unexpected error converting filter field " + field + "to correct data type"
								return nil, errors.New(msg)
							}
							arr = append(arr, v)
						}

						properties[field] = &FilterProperties{
							Field:    field,
							Operator: filterProperty.(*FilterProperties).Operator,
							Values:   arr,
						}
						break
					}
				}
			}
		}
	}
	return properties, nil
}
