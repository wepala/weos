package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	weosContext "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"golang.org/x/net/context"
)

//Context CreateHandler go context and add parameter values to context
func Context(api Container, commandDispatcher model.CommandDispatcher, repository model.EntityRepository, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			var err error
			var formErr error
			var status string
			cc := c.Request().Context()
			//get account id using the standard header
			accountID := c.Request().Header.Get(weosContext.HeaderXAccountID)
			if accountID != "" {
				cc = context.WithValue(cc, weosContext.ACCOUNT_ID, accountID)
			}
			//set the basePath in context so that urls can be generated correctly #216
			if api.GetWeOSConfig() != nil {
				cc = context.WithValue(cc, "BASE_PATH", api.GetWeOSConfig().BasePath)
			}
			//use the path information to get the parameter values
			contextValues, err := parseParams(c, path.Parameters, repository)
			//add parameter values to the context
			cc, err = AddToContext(c, cc, contextValues, repository)

			//use x-context to get parameter
			if tcontextParams, ok := operation.ExtensionProps.Extensions[ContextExtension]; ok {
				var contextParams map[string]interface{}
				err = json.Unmarshal(tcontextParams.(json.RawMessage), &contextParams)
				if err == nil {
					//use the operation information to get the parameter values
					cc, err = AddToContext(c, cc, contextParams, repository)
				}
			}
			//use the operation information to get the parameter values
			contextValues, err = parseParams(c, operation.Parameters, repository)
			//add parameter values to the context
			cc, err = AddToContext(c, cc, contextValues, repository)

			//use the operation information to get the parameter values and add them to the context

			cc, err = parseResponses(c, cc, operation)

			//add OperationID to context
			cc = context.WithValue(cc, weosContext.OPERATION_ID, operation.OperationID)

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
						case "application/x-www-form-urlencoded", "multipart/form-data":
							payload, formErr, status = ConvertFormToJson(c.Request(), ct, repository, mimeType)

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

			//This check ensures that this was not an x-upload related form error
			if formErr != nil && status == "" {
				c.Logger().Error(formErr)
			}

			//This check ensures that this was an x-upload related error
			if status == "Upload Successful" {
				cc = context.WithValue(cc, weosContext.UPLOAD_RESPONSE, "File Successfully Uploaded")
			} else if status == "Upload Failed" {
				cc = context.WithValue(cc, weosContext.UPLOAD_RESPONSE, NewControllerError(formErr.(error).Error(), formErr.(error), http.StatusBadRequest))
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
		if _, ok := schema.Properties[field]; !ok && field == "id" {
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
			}
		} else if schema.Properties[field] != nil {

			//Checks if the filter value field is not empty
			if filterProperty.(*FilterProperties).Value != nil && filterProperty.(*FilterProperties).Value.(string) != "" {
				v, err := ConvertStringToType(schema.Properties[field].Value.Type, schema.Properties[field].Value.Format, filterProperty.(*FilterProperties).Value.(string))
				if err != nil {
					msg := "unexpected error converting filter field " + field + "to correct data type"
					return nil, errors.New(msg)
				}
				properties[field] = &FilterProperties{
					Field:    field,
					Operator: filterProperty.(*FilterProperties).Operator,
					Value:    v,
				}
			}
			//checks if the length of filter values is not 0
			if len(filterProperty.(*FilterProperties).Values) != 0 {
				arr := []interface{}{}
				for _, val := range filterProperty.(*FilterProperties).Values {
					v, err := ConvertStringToType(schema.Properties[field].Value.Type, schema.Properties[field].Value.Format, val.(string))
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
			}
		}
	}
	return properties, nil
}

//ConvertFormToJson This function is used for "application/x-www-form-urlencoded" content-type to convert req body to json
func ConvertFormToJson(r *http.Request, contentType string, entityfactory model.EntityFactory, media *openapi3.MediaType) (json.RawMessage, error, string) {
	var err error
	uploadHit := false
	parsedForm := map[string]interface{}{}

	switch contentType {
	case "application/x-www-form-urlencoded":
		err = r.ParseForm()
		if err != nil {
			return nil, err, ""
		}

		for k, v := range r.PostForm {
			for _, value := range v {
				parsedForm, err = parseFormPayload(parsedForm, entityfactory.Schema(), k, value, len(v))
			}
		}

	case "multipart/form-data":

		err = r.ParseMultipartForm(1024) //Revisit
		if err != nil {
			return nil, err, ""
		}

		for k, v := range r.MultipartForm.Value {
			for _, value := range v {
				parsedForm, err = parseFormPayload(parsedForm, entityfactory.Schema(), k, value, len(v))
			}

		}

		//Checks if there was a file uploaded, also uses the properties to check for x-upload so the file can be saved to specified location
		//This allows for only the name to be saved in the payload and not the entire multipart.FileHeader struct
		if len(r.MultipartForm.File) > 0 {
			var uploadFolder map[string]interface{}

			//This check is to determine if we're dealing with an endpoint x-upload or a field x-upload
			//First we check for the endpoint x-upload
			if uploadExtension, ok := media.Schema.Value.Extensions[UploadExtension]; ok {
				_ = json.Unmarshal(uploadExtension.(json.RawMessage), &uploadFolder)

				for name, _ := range r.MultipartForm.File {
					file, header, err := r.FormFile(name)
					if err != nil {
						return nil, err, "Upload Failed"
					}
					defer file.Close()

					errr := SaveUploadedFiles(uploadFolder, file, header)
					if errr != nil {
						return nil, errr, "Upload Failed"
					}

					//This is necessary for correct response handling
					uploadHit = true

					//Adds the file path to payload instead of entire file
					parsedForm[name] = header.Filename
				}

			} else {
				//This checks if there is any x-upload defined on a property for a schema
				for name, prop := range entityfactory.Schema().Properties {
					if uploadExtension, ok := prop.Value.ExtensionProps.Extensions[UploadExtension]; ok {
						_ = json.Unmarshal(uploadExtension.(json.RawMessage), &uploadFolder)

						file, header, err := r.FormFile(name)
						if err != nil {
							return nil, err, "Upload Failed"
						}
						defer file.Close()

						errr := SaveUploadedFiles(uploadFolder, file, header)
						if errr != nil {
							return nil, errr, "Upload Failed"
						}

						//This is necessary for correct response handling
						uploadHit = true

						//Adds the file path to payload instead of entire file
						parsedForm[name] = header.Filename
					}
				}
			}
		}
	}
	parsedPayload, err := json.Marshal(parsedForm)
	if err != nil {
		return nil, err, ""
	}

	//This indicates that the upload was hit successfully and there were no errors
	if uploadHit {
		return parsedPayload, nil, "Upload Successful"
	}

	return parsedPayload, nil, ""
}

//parseFormPayload process form data and converts to a map
func parseFormPayload(parsedForm map[string]interface{}, schema *openapi3.Schema, k string, value string, valueCount int) (map[string]interface{}, error) {
	var err error
	//strip [] from the end, this allows a common query string, form pattern
	isArray := strings.Contains(k, "[")
	if isArray {
		k = strings.Replace(k, "[]", "", -1)
	}

	var temporaryValue interface{}

	//check the schema and try to convert the string to that type
	if schema != nil {
		if property, ok := schema.Properties[k]; ok {
			if property.Value != nil {
				if property.Value.Type == "array" {
					isArray = true
					var arrayValue interface{}
					if arrayValue, ok = parsedForm[k]; !ok {
						arrayValue = make([]interface{}, valueCount)
					}
					//use the type specified on "items"
					temporaryValue, err = ConvertStringToType(property.Value.Items.Value.Type, property.Value.Items.Value.Type, value)
					parsedForm[k] = append(arrayValue.([]interface{}), temporaryValue)
				} else {
					parsedForm[k], err = ConvertStringToType(property.Value.Type, property.Value.Format, value)
				}

				switch property.Value.Type {
				case "integer":
					temporaryValue, err = strconv.Atoi(value)
					if err == nil {
						//check the format and use that to convert to int32 vs int64
						switch property.Value.Format {
						case "int64":
							temporaryValue = int64(temporaryValue.(int))
						case "int32":
							temporaryValue = int32(temporaryValue.(int))
						}
					}

				case "number":
					tv, terr := strconv.ParseFloat(value, 64)
					if terr == nil {
						//check the format to determine the bit size. Default to 32 if none is specified
						if property.Value.Format != "float" {
							temporaryValue = math.Round(tv*100) / 100
						} else {
							temporaryValue = tv
						}
					}
					err = terr
				case "array":

				default:
					temporaryValue = value
				}
				//return conversion errors
				if err != nil {
					return nil, err
				} else {
					parsedForm[k] = temporaryValue
				}
			}

		}
	} else {
		//if there is more than one value then it should be stored as an array
		if isArray {
			var arrayValue interface{}
			var ok bool
			if arrayValue, ok = parsedForm[k]; !ok {
				arrayValue = []interface{}{}
			}
			parsedForm[k] = append(arrayValue.([]interface{}), value)
		} else {
			parsedForm[k] = value
		}
	}

	return parsedForm, err
}
