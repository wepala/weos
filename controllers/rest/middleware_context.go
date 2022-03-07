package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"

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
			//use the path information to get the parameter values
			contextValues, err := parseParams(c, path.Parameters, entityFactory)
			//add parameter values to the context
			cc, err = AddToContext(c, cc, contextValues, entityFactory)

			//use x-context to get parameter
			if tcontextParams, ok := operation.ExtensionProps.Extensions[ContextExtension]; ok {
				var contextParams map[string]interface{}
				err = json.Unmarshal(tcontextParams.(json.RawMessage), &contextParams)
				if err == nil {
					//use the operation information to get the parameter values
					cc, err = AddToContext(c, cc, contextParams, entityFactory)
				}
			}
			//use the operation information to get the parameter values
			contextValues, err = parseParams(c, operation.Parameters, entityFactory)
			//add parameter values to the context
			cc, err = AddToContext(c, cc, contextValues, entityFactory)

			//use the operation information to get the parameter values and add them to the context

			cc, err = parseResponses(c, cc, operation)

			//parse request body based on content type
			var payload []byte
			ct := c.Request().Header.Get("Content-Type")
			//split the content type on ; and use the first segment. This is based on multidata
			ctParts := strings.Split(ct, ";")
			ct = ctParts[0]

			if operation != path.Delete {
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

//parseResponses gets the expected response for cases where different valid responses are possible
func parseResponses(c echo.Context, cc context.Context, operation *openapi3.Operation) (context.Context, error) {
	for code, r := range operation.Responses {
		if r.Value != nil {
			for contentName, _ := range r.Value.Content {
				cc = context.WithValue(cc, weosContext.RESPONSE_PREFIX+code, contentName)
			}
		}
	}
	return cc, nil
}

//parseParams uses the parameter type to determine where to pull the value from
func parseParams(c echo.Context, parameters openapi3.Parameters, entityFactory model.EntityFactory) (map[string]interface{}, error) {
	var errors error
	contextValues := map[string]interface{}{}
	//get the parameters from the requests
	for _, parameter := range parameters {
		if parameter.Value != nil {
			contextName := parameter.Value.Name
			paramType := parameter.Value.Schema
			//if there is a context name specified use that instead. The value is a json.RawMessage (not a string)
			if tcontextName, ok := parameter.Value.ExtensionProps.Extensions[ContextNameExtension]; ok {
				err := json.Unmarshal(tcontextName.(json.RawMessage), &contextName)
				if err != nil {
					errors = err
					continue
				}
			}
			//if there is an alias name specified use that instead. The value is a json.RawMessage (not a string)
			if tcontextName, ok := parameter.Value.ExtensionProps.Extensions[AliasExtension]; ok {
				err := json.Unmarshal(tcontextName.(json.RawMessage), &contextName)
				if err != nil {
					errors = err
					continue
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
				switch contextName {
				case "_filters":
					val = c.Request().URL.RawQuery
					if val.(string) == "" {
						delete(contextValues, contextName)
					}
				default:
					if paramType != nil && paramType.Value != nil {
						pType := paramType.Value.Type
						switch strings.ToLower(pType) {
						case "integer":
							if val.(string) == "" {
								delete(contextValues, contextName)
								break
							}
							v, err := strconv.Atoi(val.(string))
							if err == nil {
								val = v
							}
						case "boolean":
							if val.(string) == "" {
								delete(contextValues, contextName)
								break
							}
							v, err := strconv.ParseBool(val.(string))
							if err == nil {
								val = v
							}
						case "number":
							if val.(string) == "" {
								delete(contextValues, contextName)
								break
							}
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
			contextValues[contextName] = val
		}
	}

	//TODO account for $ref tag reference
	return contextValues, errors
}

func AddToContext(c echo.Context, cc context.Context, contextValues map[string]interface{}, entityFactory model.EntityFactory) (context.Context, error) {
	if contextValues == nil {
		return cc, nil
	}
	var errors error
	for key, value := range contextValues {
		switch key {
		case "sequence_no", "page", "limit": //default type is integer
			switch value.(type) {
			case float64:
				contextValues[key] = int(value.(float64))
			case string:
				if value.(string) == "" {
					delete(contextValues, key)
					break
				}
				v, err := strconv.Atoi(value.(string))
				if err == nil {
					contextValues[key] = v
				}
			}
		case "use_entity_id":
			if val, ok := value.(string); ok {
				//default type is boolean
				if value.(string) == "" {
					delete(contextValues, key)
					break
				}
				v, err := strconv.ParseBool(val)
				if err == nil {
					contextValues[key] = v
				}
			}
		case "_filters":
			if value == nil {
				errors = fmt.Errorf("unexpected error no filters specified")
				continue
			}
			if val, ok := value.(string); ok {
				if value.(string) == "" {
					delete(contextValues, key)
					break

				}
				//if the filter comes from x-context do this conversion
				filters := map[string]interface{}{}
				decodedQuery, err := url.PathUnescape(val)
				if err != nil {
					errors = fmt.Errorf("Error decoding the string %v", err)
					continue
				}
				filtersArray := SplitFilters(decodedQuery)
				if filtersArray != nil && len(filtersArray) > 0 {
					for _, value := range filtersArray {
						if strings.Contains(value, "_filters") {
							prop := SplitFilter(value)
							if prop == nil {
								errors = fmt.Errorf("unexpected error filter format is incorrect: %s", value)
								break
							}
							filters[prop.Field] = prop
						}
					}
					filters, err = convertProperties(filters, entityFactory.Schema())
					if err != nil {
						errors = err
						continue
					}
					contextValues[key] = filters
					continue
				}

			} else {
				//if the filter comes from request do this conversion
				if value.([]interface{}) == nil {
					delete(contextValues, key)
					break
				}
				filters := map[string]interface{}{}
				for _, filterProp := range value.([]interface{}) {
					if filterProp.(map[string]interface{})["operator"] == nil || filterProp.(map[string]interface{})["field"] == nil || (filterProp.(map[string]interface{})["value"] == nil && filterProp.(map[string]interface{})["values"] == nil) {
						errors = fmt.Errorf("unexpected error all filter fields are not filled out")
						break
					}
					if filterProp.(map[string]interface{})["values"] != nil {
						filters[filterProp.(map[string]interface{})["field"].(string)] = &FilterProperties{
							Field:    filterProp.(map[string]interface{})["field"].(string),
							Operator: filterProp.(map[string]interface{})["operator"].(string),
							Values:   filterProp.(map[string]interface{})["values"].([]interface{}),
						}
					} else {
						filters[filterProp.(map[string]interface{})["field"].(string)] = &FilterProperties{
							Field:    filterProp.(map[string]interface{})["field"].(string),
							Operator: filterProp.(map[string]interface{})["operator"].(string),
							Value:    filterProp.(map[string]interface{})["value"],
						}
					}
				}
				if len(value.([]interface{})) != len(filters) {
					continue
				}
				contextValues[key] = filters
			}
		case "If-Match", "If-None-Match": //default type is string
			if value != nil {
				if value.(string) == "" {
					delete(contextValues, key)
					break
				}
				contextValues[key] = value.(string)
			}
		case "_sorts":
			if value.([]interface{}) == nil {
				delete(contextValues, key)
				break
			}
			sortOptions := map[string]string{}
			for _, sortOption := range value.([]interface{}) {
				if sortOption.(map[string]interface{})["field"] == nil || sortOption.(map[string]interface{})["order"] == nil {
					errors = fmt.Errorf("unexpected error all sort fields are not filled out")
					continue
				}
				sortOptions[sortOption.(map[string]interface{})["field"].(string)] = sortOption.(map[string]interface{})["field"].(string)
			}
			contextValues[key] = sortOptions
		}
	}
	for key, value := range contextValues {
		cc = context.WithValue(cc, key, value)
	}
	return cc, errors
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
