package rest

import (
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	ds "github.com/ompluscator/dynamic-struct"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"golang.org/x/net/context"
	"net/http"
	"regexp"
	"strings"
)

//ContextInitializer add context middleware to path
func ContextInitializer(ctxt context.Context, api *RESTAPI, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
	middlewares := GetOperationMiddlewares(ctxt)
	contextMiddleware, err := api.GetMiddleware("Context")
	if err != nil {
		return ctxt, err
	}
	middlewares = append(middlewares, contextMiddleware)
	ctxt = context.WithValue(ctxt, weoscontext.MIDDLEWARES, middlewares)
	return ctxt, nil
}

//EntityFactoryInitializer setups the EntityFactory for a specific route
func EntityFactoryInitializer(ctxt context.Context, api *RESTAPI, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
	schemas := GetSchemaBuilders(ctxt)
	jsonSchema := operation.ExtensionProps.Extensions[SchemaExtension]
	if jsonSchema != nil {
		contentType := ""
		err := json.Unmarshal(jsonSchema.(json.RawMessage), &contentType)
		if err != nil {
			return ctxt, err
		}

		if strings.Contains(contentType, "#/components/schemas/") {
			contentType = strings.Replace(contentType, "#/components/schemas/", "", -1)
		}

		//get the schema details from the swagger file
		if builder, ok := schemas[contentType]; ok {
			entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, swagger.Components.Schemas[contentType].Value, builder)
			newContext := context.WithValue(ctxt, weoscontext.ENTITY_FACTORY, entityFactory)
			api.RegisterEntityFactory(entityFactory.Name(), entityFactory)
			return newContext, nil
		}

	}
	if operation.RequestBody != nil {
		//get the entity information based on the Content Type associated with this operation
		for _, requestContent := range operation.RequestBody.Value.Content {
			if requestContent.Schema != nil {
				//use the first schema ref to determine the entity type
				if requestContent.Schema.Ref != "" {
					contentType := strings.Replace(requestContent.Schema.Ref, "#/components/schemas/", "", -1)
					//get the schema details from the swagger file
					if builder, ok := schemas[contentType]; ok {
						entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, swagger.Components.Schemas[contentType].Value, builder)
						newContext := context.WithValue(ctxt, weoscontext.ENTITY_FACTORY, entityFactory)
						api.RegisterEntityFactory(entityFactory.Name(), entityFactory)
						return newContext, nil
					}
					break
				}
				//use the first schema ref to determine the entity type
				if requestContent.Schema.Value.Items != nil && strings.Contains(requestContent.Schema.Value.Items.Ref, "#/components/schemas/") {
					contentType := strings.Replace(requestContent.Schema.Value.Items.Ref, "#/components/schemas/", "", -1)
					if builder, ok := schemas[contentType]; ok {
						entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, swagger.Components.Schemas[contentType].Value, builder)
						newContext := context.WithValue(ctxt, weoscontext.ENTITY_FACTORY, entityFactory)
						api.RegisterEntityFactory(entityFactory.Name(), entityFactory)
						return newContext, nil
					}
				}
			}
		}
	}

	if operation.Responses.Get(http.StatusOK) != nil {
		for _, respContent := range operation.Responses.Get(http.StatusOK).Value.Content {
			if respContent.Schema != nil {
				//use the first schema ref to determine the entity type
				if respContent.Schema.Ref != "" {
					contentType := strings.Replace(respContent.Schema.Ref, "#/components/schemas/", "", -1)
					if builder, ok := schemas[contentType]; ok {
						entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, swagger.Components.Schemas[contentType].Value, builder)
						newContext := context.WithValue(ctxt, weoscontext.ENTITY_FACTORY, entityFactory)
						api.RegisterEntityFactory(entityFactory.Name(), entityFactory)
						return newContext, nil
					}
				}
				//use the first schema ref to determine the entity type
				if respContent.Schema.Value.Properties["items"] != nil && respContent.Schema.Value.Properties["items"].Value.Items != nil {
					contentType := strings.Replace(respContent.Schema.Value.Properties["items"].Value.Items.Ref, "#/components/schemas/", "", -1)
					if builder, ok := schemas[contentType]; ok {
						entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, swagger.Components.Schemas[contentType].Value, builder)
						newContext := context.WithValue(ctxt, weoscontext.ENTITY_FACTORY, entityFactory)
						api.RegisterEntityFactory(entityFactory.Name(), entityFactory)
						return newContext, nil
					}
				} else {
					//if items are named differently the alias is checked
					var alias string
					for _, prop := range respContent.Schema.Value.Properties {
						aliasInterface := prop.Value.ExtensionProps.Extensions[AliasExtension]
						if aliasInterface != nil {
							bytesContext := aliasInterface.(json.RawMessage)
							json.Unmarshal(bytesContext, &alias)
							if alias == "items" {
								if prop.Value.Type == "array" && prop.Value.Items != nil && strings.Contains(prop.Value.Items.Ref, "#/components/schemas/") {
									contentType := strings.Replace(prop.Value.Items.Ref, "#/components/schemas/", "", -1)
									if builder, ok := schemas[contentType]; ok {
										entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType, swagger.Components.Schemas[contentType].Value, builder)
										newContext := context.WithValue(ctxt, weoscontext.ENTITY_FACTORY, entityFactory)
										api.RegisterEntityFactory(entityFactory.Name(), entityFactory)
										return newContext, nil
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return ctxt, nil
}

//UserDefinedInitializer adds user defined middleware, controller, command dispatchers and event store to the initialize context
func UserDefinedInitializer(ctxt context.Context, api *RESTAPI, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
	//if the controller extension is set then add controller to the context
	if controllerExtension, ok := operation.ExtensionProps.Extensions[ControllerExtension]; ok {
		controllerName := ""
		err := json.Unmarshal(controllerExtension.(json.RawMessage), &controllerName)
		if err != nil {
			return ctxt, err
		}
		controller, err := api.GetController(controllerName)
		if err != nil {
			return ctxt, fmt.Errorf("unregistered controller '%s' specified on path '%s'", controllerName, path)
		}
		ctxt = context.WithValue(ctxt, weoscontext.CONTROLLER, controller)
		attached := false
		//checks if the controller explicitly stated and whether the endpoint is valid
		if strings.ToUpper(method) == "GET" && controllerName == "ListController" {
			if pathItem.Get.Responses != nil && pathItem.Get.Responses["200"].Value.Content != nil {
				for _, val := range pathItem.Get.Responses["200"].Value.Content {
					//checks if the response refers to an array schema
					if val.Schema.Value.Properties != nil && val.Schema.Value.Properties["items"] != nil && val.Schema.Value.Properties["items"].Value.Type == "array" && val.Schema.Value.Properties["items"].Value.Items != nil && strings.Contains(val.Schema.Value.Properties["items"].Value.Items.Ref, "#/components/schemas/") {
						attached = true
						break
					}

				}
				if !attached {
					ctxt = context.WithValue(ctxt, weoscontext.CONTROLLER, nil)
					api.e.Logger.Warnf("no handler set, path: '%s' operation '%s'", path, method)
				}
			}
		}
	}

	//if the controller extension is set then add controller to the context
	if middlewareExtension, ok := operation.ExtensionProps.Extensions[MiddlewareExtension]; ok {
		var middlewareNames []string
		err := json.Unmarshal(middlewareExtension.(json.RawMessage), &middlewareNames)
		if err != nil {
			api.EchoInstance().Logger.Errorf("unable to unmarshal middleware '%s'", err)
			return ctxt, fmt.Errorf("middlewares in the specification should be an array of strings on '%s'", path)
		}
		//get the existing middleware from context and then add user defined middleware to it
		middlewares := GetOperationMiddlewares(ctxt)
		for _, middlewareName := range middlewareNames {
			middleware, err := api.GetMiddleware(middlewareName)
			if err != nil {
				return ctxt, fmt.Errorf("unregistered middleware '%s' specified on path '%s'", middlewareName, path)
			}
			middlewares = append(middlewares, middleware)
		}
		ctxt = context.WithValue(ctxt, weoscontext.MIDDLEWARES, middlewares)
	}

	if projectionExtension, ok := operation.ExtensionProps.Extensions[ProjectionExtension]; ok {
		projectionName := ""
		err := json.Unmarshal(projectionExtension.(json.RawMessage), &projectionName)
		if err != nil {
			return ctxt, err
		}
		projection, err := api.GetProjection(projectionName)
		if err != nil {
			return ctxt, fmt.Errorf("unregistered projection '%s' specified on path '%s'", projectionName, path)
		}
		ctxt = context.WithValue(ctxt, weoscontext.PROJECTION, projection)
	}

	if commandDispatcherExtension, ok := operation.ExtensionProps.Extensions[CommandDispatcherExtension]; ok {
		commandDispatcherName := ""
		err := json.Unmarshal(commandDispatcherExtension.(json.RawMessage), &commandDispatcherName)
		if err != nil {
			return ctxt, err
		}
		commandDispatcher, err := api.GetCommandDispatcher(commandDispatcherName)
		if err != nil {
			return ctxt, fmt.Errorf("unregistered command dispatcher '%s' specified on path '%s'", commandDispatcherName, path)
		}
		ctxt = context.WithValue(ctxt, weoscontext.COMMAND_DISPATCHER, commandDispatcher)
	}

	if eventStoreExtension, ok := operation.ExtensionProps.Extensions[EventStoreExtension]; ok {
		eventStoreName := ""
		err := json.Unmarshal(eventStoreExtension.(json.RawMessage), &eventStoreName)
		if err != nil {
			return ctxt, err
		}
		eventStore, err := api.GetEventStore(eventStoreName)
		if err != nil {
			return ctxt, fmt.Errorf("unregistered command dispatcher '%s' specified on path '%s'", eventStoreName, path)
		}
		ctxt = context.WithValue(ctxt, weoscontext.EVENT_STORE, eventStore)
	}
	return ctxt, nil
}

//StandardInitializer adds standard controller and middleware if not already setup
func StandardInitializer(ctxt context.Context, api *RESTAPI, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
	if GetOperationController(ctxt) == nil {
		autoConfigure := false
		handler := ""
		middlewareNames := make(map[string]bool)
		switch strings.ToUpper(method) {
		case "POST":
			if pathItem.Post.RequestBody == nil {
				api.e.Logger.Warnf("unexpected error: expected request body but got nil")
				break
			} else {
				//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
				for _, value := range pathItem.Post.RequestBody.Value.Content {
					if value.Schema != nil && strings.Contains(value.Schema.Ref, "#/components/schemas/") {
						handler = "CreateController"
						middlewareNames["CreateMiddleware"] = true
						autoConfigure = true
					} else if value.Schema.Value.Type == "array" && value.Schema.Value.Items != nil && strings.Contains(value.Schema.Value.Items.Ref, "#/components/schemas/") {
						attach := true
						for _, compare := range pathItem.Post.RequestBody.Value.Content {
							if compare.Schema.Value.Items.Ref != value.Schema.Value.Items.Ref {
								api.e.Logger.Warnf("unexpected error: cannot assign different schemas for different content types")
								attach = false
								break
							}
						}
						if attach {
							handler = "CreateBatchController"
							middlewareNames["CreateBatchMiddleware"] = true
							autoConfigure = true
						}

					}

				}
			}
		case "PUT":
			allParam := true
			if pathItem.Put.RequestBody == nil {
				break
			} else {
				//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
				for _, value := range pathItem.Put.RequestBody.Value.Content {
					if value.Schema != nil && strings.Contains(value.Schema.Ref, "#/components/schemas/") {
						var identifiers []string
						identifierExtension := swagger.Components.Schemas[strings.Replace(value.Schema.Ref, "#/components/schemas/", "", -1)].Value.ExtensionProps.Extensions[IdentifierExtension]
						if identifierExtension != nil {
							bytesId := identifierExtension.(json.RawMessage)
							json.Unmarshal(bytesId, &identifiers)
						}
						var contextName string
						//check for identifiers
						if identifiers != nil && len(identifiers) > 0 {
							for _, identifier := range identifiers {
								foundIdentifier := false
								//check the parameters for the identifiers
								for _, param := range pathItem.Put.Parameters {
									cName := param.Value.ExtensionProps.Extensions[ContextNameExtension]
									if identifier == param.Value.Name || (cName != nil && identifier == cName.(string)) {
										foundIdentifier = true
										break
									}
								}
								if !foundIdentifier {
									allParam = false
									api.e.Logger.Warnf("unexpected error: a parameter for each part of the identifier must be set")
									break
								}
							}
							if allParam {
								handler = "UpdateController"
								middlewareNames["UpdateMiddleware"] = true
								autoConfigure = true
								break
							}
						} else {
							//if there is no identifiers then id is the default identifier
							for _, param := range pathItem.Put.Parameters {

								if "id" == param.Value.Name {
									handler = "UpdateController"
									middlewareNames["UpdateMiddleware"] = true
									autoConfigure = true
									break
								}
								interfaceContext := param.Value.ExtensionProps.Extensions[ContextNameExtension]
								if interfaceContext != nil {
									bytesContext := interfaceContext.(json.RawMessage)
									json.Unmarshal(bytesContext, &contextName)
									if "id" == contextName {
										handler = "UpdateController"
										middlewareNames["UpdateMiddleware"] = true
										autoConfigure = true
										break
									}
								}
							}
						}
					}
				}
			}

		case "PATCH":
			allParam := true
			if pathItem.Patch.RequestBody == nil {
				break
			} else {
				//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
				for _, value := range pathItem.Patch.RequestBody.Value.Content {
					if value.Schema != nil && strings.Contains(value.Schema.Ref, "#/components/schemas/") {
						var identifiers []string
						identifierExtension := swagger.Components.Schemas[strings.Replace(value.Schema.Ref, "#/components/schemas/", "", -1)].Value.ExtensionProps.Extensions[IdentifierExtension]
						if identifierExtension != nil {
							bytesId := identifierExtension.(json.RawMessage)
							json.Unmarshal(bytesId, &identifiers)
						}
						var contextName string
						//check for identifiers
						if identifiers != nil && len(identifiers) > 0 {
							for _, identifier := range identifiers {
								//check the parameters for the identifiers
								for _, param := range pathItem.Patch.Parameters {
									cName := param.Value.ExtensionProps.Extensions[ContextNameExtension]
									if identifier == param.Value.Name || (cName != nil && identifier == cName.(string)) {
										break
									}
									if !(identifier == param.Value.Name) && !(cName != nil && identifier == cName.(string)) {
										allParam = false
										api.e.Logger.Warnf("unexpected error: a parameter for each part of the identifier must be set")
										break
									}
								}
							}
							if allParam {
								handler = "UpdateController"
								middlewareNames["UpdateMiddleware"] = true
								autoConfigure = true
								break
							}
						} else {
							//if there is no identifiers then id is the default identifier
							for _, param := range pathItem.Patch.Parameters {

								if "id" == param.Value.Name {
									handler = "UpdateController"
									middlewareNames["UpdateMiddleware"] = true
									autoConfigure = true
									break
								}
								interfaceContext := param.Value.ExtensionProps.Extensions[ContextNameExtension]
								if interfaceContext != nil {
									bytesContext := interfaceContext.(json.RawMessage)
									json.Unmarshal(bytesContext, &contextName)
									if "id" == contextName {
										handler = "UpdateController"
										middlewareNames["UpdateMiddleware"] = true
										autoConfigure = true
										break
									}
								}
							}
						}
					}
				}
			}
		case "GET":
			allParam := true
			//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
			//checks if the response refers to a schema
			if pathItem.Get != nil && pathItem.Get.Responses != nil && pathItem.Get.Responses["200"] != nil && pathItem.Get.Responses["200"].Value.Content != nil {
				for _, val := range pathItem.Get.Responses["200"].Value.Content {
					if val.Schema != nil && strings.Contains(val.Schema.Ref, "#/components/schemas/") {
						var identifiers []string
						identifierExtension := swagger.Components.Schemas[strings.Replace(val.Schema.Ref, "#/components/schemas/", "", -1)].Value.ExtensionProps.Extensions[IdentifierExtension]
						if identifierExtension != nil {
							bytesId := identifierExtension.(json.RawMessage)
							err := json.Unmarshal(bytesId, &identifiers)
							if err != nil {
								return ctxt, err
							}
						}
						var contextName string
						if identifiers != nil && len(identifiers) > 0 {
							for _, identifier := range identifiers {
								foundIdentifier := false
								//check the parameters
								for _, param := range pathItem.Get.Parameters {
									cName := param.Value.ExtensionProps.Extensions[ContextNameExtension]
									if identifier == param.Value.Name || (cName != nil && identifier == cName.(string)) {
										foundIdentifier = true
										break
									}
								}
								if !foundIdentifier {
									allParam = false
									api.e.Logger.Warnf("unexpected error: a parameter for each part of the identifier must be set")
									break
								}
							}
						} else {
							//check the parameters for id
							if pathItem.Get.Parameters != nil && len(pathItem.Get.Parameters) != 0 {
								for _, param := range pathItem.Get.Parameters {
									if "id" == param.Value.Name {
										allParam = true
									}
									contextInterface := param.Value.ExtensionProps.Extensions[ContextNameExtension]
									if contextInterface != nil {
										bytesContext := contextInterface.(json.RawMessage)
										json.Unmarshal(bytesContext, &contextName)
										if "id" == contextName {
											allParam = true
										}
									}
								}
							}
						}
						if allParam {
							handler = "ViewController"
							middlewareNames["ViewMiddleware"] = true
							autoConfigure = true
							break
						}
					} else {
						//checks if the response refers to an array schema
						if val.Schema != nil && val.Schema.Value.Properties != nil && val.Schema.Value.Properties["items"] != nil && val.Schema.Value.Properties["items"].Value.Type == "array" && val.Schema.Value.Properties["items"].Value.Items != nil && strings.Contains(val.Schema.Value.Properties["items"].Value.Items.Ref, "#/components/schemas/") {
							handler = "ListController"
							middlewareNames["ListMiddleware"] = true
							autoConfigure = true
							break
						} else {
							if val.Schema != nil && val.Schema.Value.Properties != nil {
								var alias string
								for _, prop := range val.Schema.Value.Properties {
									aliasInterface := prop.Value.ExtensionProps.Extensions[AliasExtension]
									if aliasInterface != nil {
										bytesContext := aliasInterface.(json.RawMessage)
										json.Unmarshal(bytesContext, &alias)
										if alias == "items" {
											if prop.Value.Type == "array" && prop.Value.Items != nil && strings.Contains(prop.Value.Items.Ref, "#/components/schemas/") {
												handler = "ListController"
												middlewareNames["ListMiddleware"] = true
												autoConfigure = true
												break
											}
										}
									}
								}
							}
						}
					}

				}
			}
		case "DELETE":
			var strContentType string
			allParam := true
			contentTypeExt := pathItem.Delete.ExtensionProps.Extensions[SchemaExtension]

			if pathItem.Delete.RequestBody == nil && contentTypeExt == nil {
				break
			}

			var identifiers []string
			var contextName string
			var identifierExtension interface{}

			if contentTypeExt != nil {
				jsonContentType := contentTypeExt.(json.RawMessage)
				err := json.Unmarshal(jsonContentType, &strContentType)
				if err != nil {
					api.e.Logger.Errorf("error on path '%s' '%s' ", path, err)
					return ctxt, err
				}

				if strings.Contains(strContentType, "#/components/schemas/") {
					identifierExtension = swagger.Components.Schemas[strings.Replace(strContentType, "#/components/schemas/", "", -1)].Value.ExtensionProps.Extensions[IdentifierExtension]
				} else {
					identifierExtension = swagger.Components.Schemas[strContentType].Value.ExtensionProps.Extensions[IdentifierExtension]
				}
			} else {
				//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
				for _, value := range pathItem.Delete.RequestBody.Value.Content {
					if !strings.Contains(value.Schema.Ref, "#/components/schemas/") {
						api.e.Logger.Warnf("no handler set, path: '%s' operation '%s'", path, method)
						return ctxt, nil
					}
					identifierExtension = swagger.Components.Schemas[strings.Replace(value.Schema.Ref, "#/components/schemas/", "", -1)].Value.ExtensionProps.Extensions[IdentifierExtension]
					break
				}
			}

			if identifierExtension != nil {
				bytesId := identifierExtension.(json.RawMessage)
				json.Unmarshal(bytesId, &identifiers)
			}
			//check for identifiers
			if identifiers != nil && len(identifiers) > 0 {
				for _, identifier := range identifiers {
					foundIdentifier := false
					//check the parameters for the identifiers
					for _, param := range pathItem.Delete.Parameters {
						cName := param.Value.ExtensionProps.Extensions[ContextNameExtension]
						if identifier == param.Value.Name || (cName != nil && identifier == cName.(string)) {
							foundIdentifier = true
							break
						}
					}
					if !foundIdentifier {
						allParam = false
						api.e.Logger.Warnf("unexpected error: a parameter for each part of the identifier must be set")
						return ctxt, nil
					}
				}
				if allParam {
					handler = "DeleteController"
					middlewareNames["DeleteMiddleware"] = true
					autoConfigure = true
					break
				}
			}
			//if there is no identifiers then id is the default identifier
			for _, param := range pathItem.Delete.Parameters {

				if "id" == param.Value.Name {
					handler = "DeleteController"
					middlewareNames["DeleteMiddleware"] = true
					autoConfigure = true
					break
				}
				interfaceContext := param.Value.ExtensionProps.Extensions[ContextNameExtension]
				if interfaceContext != nil {
					bytesContext := interfaceContext.(json.RawMessage)
					json.Unmarshal(bytesContext, &contextName)
					if "id" == contextName {
						handler = "DeleteController"
						middlewareNames["DeleteMiddleware"] = true
						autoConfigure = true
						break
					}
				}
			}
		}

		if handler != "" && autoConfigure {
			controller, err := api.GetController(handler)
			if err != nil {
				api.e.Logger.Warnf("unexpected error initializing controller: %s", err)
				return ctxt, fmt.Errorf("controller '%s' set on path '%s' not found", handler, path)
			}
			if controller != nil {
				ctxt = context.WithValue(ctxt, weoscontext.CONTROLLER, controller)
			}
		} else {
			//this should not return an error it should log
			api.e.Logger.Warnf("no handler set, path: '%s' operation '%s'", path, method)
			//once no controller is set, the default controller and middleware is added to the path
			controller, err := api.GetController("DefaultResponseController")
			if err != nil {
				api.e.Logger.Warnf("unexpected error initializing controller: %s", err)
				return ctxt, fmt.Errorf("controller '%s' set on path '%s' not found", handler, path)
			}
			if controller != nil {
				ctxt = context.WithValue(ctxt, weoscontext.CONTROLLER, controller)
			}
			middlewareNames["DefaultResponseMiddleware"] = true

		}
		middlewares := GetOperationMiddlewares(ctxt)
		//there are middlewareNames let's add them
		for middlewareName := range middlewareNames {
			if middleware, _ := api.GetMiddleware(middlewareName); middleware != nil {
				middlewares = append(middlewares, middleware)
			}
		}
		ctxt = context.WithValue(ctxt, weoscontext.MIDDLEWARES, middlewares)
	}

	return ctxt, nil
}

//RouteInitializer creates route using information in the initialization context
func RouteInitializer(ctxt context.Context, api *RESTAPI, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
	var err error
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	echoPath := re.ReplaceAllString(path, `:$1`)
	controller := GetOperationController(ctxt)
	projection := GetOperationProjection(ctxt)
	if projection == nil {
		projection, err = api.GetProjection("Default")

	}
	commandDispatcher := GetOperationCommandDispatcher(ctxt)
	if commandDispatcher == nil {
		commandDispatcher, err = api.GetCommandDispatcher("Default")
		if commandDispatcher == nil {
			return ctxt, fmt.Errorf("command dispatcher must be configured. No default found '%s'", err)
		}
	}
	eventStore := GetOperationEventStore(ctxt)
	if eventStore == nil {
		eventStore, err = api.GetEventStore("Default")
	}
	entityFactory := GetEntityFactory(ctxt)
	if entityFactory == nil {

	}
	//only set up routes if controller is set because echo returns an error if the handler for a route is nil
	if controller != nil {
		var handler echo.HandlerFunc
		handler = controller(api, projection, commandDispatcher, eventStore, entityFactory)
		middlewares := GetOperationMiddlewares(ctxt)
		var pathMiddleware []echo.MiddlewareFunc
		for _, tmiddleware := range middlewares {
			//Not sure if CORS middleware and any other middlewares needs to be added
			pathMiddleware = append(pathMiddleware, tmiddleware(api, projection, commandDispatcher, eventStore, entityFactory, pathItem, operation))
		}
		if controllerExtension, ok := operation.ExtensionProps.Extensions[ControllerExtension]; ok {
			controllerName := ""
			err := json.Unmarshal(controllerExtension.(json.RawMessage), &controllerName)
			if err != nil {
				return ctxt, err
			}
			if controllerName == "APIDiscovery" {
				//make default endpoints for returning swagger configuration to user
				api.RegisterDefaultSwaggerAPI(pathMiddleware)
				api.RegisterDefaultSwaggerJSON(pathMiddleware)
			}

		}
		switch method {
		case "GET":
			api.EchoInstance().GET(api.Config.BasePath+echoPath, handler, pathMiddleware...)
		case "POST":
			api.e.POST(api.Config.BasePath+echoPath, handler, pathMiddleware...)
		case "PUT":
			api.e.PUT(api.Config.BasePath+echoPath, handler, pathMiddleware...)
		case "PATCH":
			api.e.PATCH(api.Config.BasePath+echoPath, handler, pathMiddleware...)
		case "DELETE":
			api.e.DELETE(api.Config.BasePath+echoPath, handler, pathMiddleware...)
		case "HEAD":
			api.e.HEAD(api.Config.BasePath+echoPath, handler, pathMiddleware...)
		case "TRACE":
			api.e.TRACE(api.Config.BasePath+echoPath, handler, pathMiddleware...)
		case "CONNECT":
			api.e.CONNECT(api.Config.BasePath+echoPath, handler, pathMiddleware...)

		}
	}

	return ctxt, err
}

func GetOperationMiddlewares(ctx context.Context) []Middleware {
	if value, ok := ctx.Value(weoscontext.MIDDLEWARES).([]Middleware); ok {
		return value
	}
	return []Middleware{}
}

func GetOperationController(ctx context.Context) Controller {
	if value, ok := ctx.Value(weoscontext.CONTROLLER).(Controller); ok {
		return value
	}
	return nil
}

func GetOperationCommandDispatcher(ctx context.Context) model.CommandDispatcher {
	if value, ok := ctx.Value(weoscontext.COMMAND_DISPATCHER).(model.CommandDispatcher); ok {
		return value
	}
	return nil
}

func GetOperationEventStore(ctx context.Context) model.EventRepository {
	if value, ok := ctx.Value(weoscontext.EVENT_STORE).(model.EventRepository); ok {
		return value
	}
	return nil
}

func GetOperationProjection(ctx context.Context) projections.Projection {
	if value, ok := ctx.Value(weoscontext.PROJECTION).(projections.Projection); ok {
		return value
	}
	return nil
}

//GetEntityFactory get the configured event factory from the context
func GetEntityFactory(ctx context.Context) model.EntityFactory {
	if value, ok := ctx.Value(weoscontext.ENTITY_FACTORY).(model.EntityFactory); ok {
		return value
	}
	return nil
}

//GetSchemaBuilders get a map of the dynamic struct builders for the schemas from the context
func GetSchemaBuilders(ctx context.Context) map[string]ds.Builder {
	if value, ok := ctx.Value(weoscontext.SCHEMA_BUILDERS).(map[string]ds.Builder); ok {
		return value
	}
	return make(map[string]ds.Builder)
}
