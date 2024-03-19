package rest

import (
	"encoding/json"
	"fmt"
	"github.com/casbin/casbin/v2"
	casbinmodel "github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	ds "github.com/ompluscator/dynamic-struct"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
)

// ContextInitializer add context middleware to path
func ContextInitializer(ctxt context.Context, api Container, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
	middlewares := GetOperationMiddlewares(ctxt)
	contextMiddleware, err := api.GetMiddleware("Context")
	if err != nil {
		return ctxt, err
	}
	middlewares = append(middlewares, contextMiddleware)
	ctxt = context.WithValue(ctxt, weoscontext.MIDDLEWARES, middlewares)
	return ctxt, nil
}

// AuthorizationInitializer setup authorization
func AuthorizationInitializer(ctxt context.Context, tapi Container, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
	if authRaw, ok := operation.Extensions[AuthorizationConfigExtension]; ok {
		var enforcer *casbin.Enforcer
		var err error

		//get default logger
		log, err := tapi.GetLog("Default")
		if err != nil {
			return ctxt, err
		}

		defer func() {
			if err1 := recover(); err1 != nil {
				log.Error("panic occurred ", string(debug.Stack()))
			}
		}()

		//update path so that the open api way of specifying url parameters is change to wildcards. This is to support the casbin policy
		//note ideal we would use the open api way of specifying url parameters but this is not supported by casbin
		re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
		path = re.ReplaceAllString(path, `*`)

		//check if the default enforcer is setup
		if enforcer, err = tapi.GetPermissionEnforcer("Default"); err != nil {

			var adapter interface{}
			if gormDB, err := tapi.GetGormDBConnection("Default"); err == nil {
				adapter, _ = gormadapter.NewAdapterByDB(gormDB)
			} else {
				adapter = "./policy.csv"
			}

			//default REST permission model
			text := `[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && keyMatch(r.obj, p.obj) && regexMatch(r.act, p.act)
`
			m, _ := casbinmodel.NewModelFromString(text)
			enforcer, err = casbin.NewEnforcer(m, adapter)
			if err != nil {
				return ctxt, err
			}
			tapi.RegisterPermissionEnforcer("Default", enforcer)
		}
		//add rule to the enforcer based on the operation
		var authConfig map[string]interface{}
		if err = json.Unmarshal(authRaw.(json.RawMessage), &authConfig); err == nil {
			if allowRules, ok := authConfig["allow"]; ok {
				//setup users
				if u, ok := allowRules.(map[string]interface{})["users"]; ok {
					for _, user := range u.([]interface{}) {
						if user == nil {
							log.Warnf("user is nil on path '%s' for method '%s'", path, method)
							continue
						}
						var success bool
						success, err = enforcer.AddPolicy(user.(string), path, method)
						if !success {
							//TODO show warning to developer or something
						}
					}
				}
				//setup roles
				if u, ok := allowRules.(map[string]interface{})["roles"]; ok {
					for _, user := range u.([]interface{}) {
						var success bool
						if user == nil {
							log.Warnf("user is nil on path '%s' for method '%s'", path, method)
							continue
						}
						success, err = enforcer.AddPolicy(user.(string), path, method)
						if !success {
							//TODO show warning to developer or something
						}
					}
				}
			}
		}
		return ctxt, err
	}
	return ctxt, nil
}

// EntityRepositoryInitializer setups the EntityFactory for a specific route
func EntityRepositoryInitializer(ctxt context.Context, api Container, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
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
		if _, ok := swagger.Components.Schemas[contentType]; ok {
			repository, err := api.GetEntityRepository(contentType)
			if err != nil {
				return ctxt, err
			}
			newContext := context.WithValue(ctxt, weoscontext.ENTITY_REPOSITORY, repository)
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
					if _, ok := swagger.Components.Schemas[contentType]; ok {
						repository, err := api.GetEntityRepository(contentType)
						if err != nil {
							return ctxt, err
						}
						newContext := context.WithValue(ctxt, weoscontext.ENTITY_REPOSITORY, repository)
						return newContext, nil
					}
					break
				}
				//use the first schema ref to determine the entity type
				if requestContent.Schema.Value.Items != nil && strings.Contains(requestContent.Schema.Value.Items.Ref, "#/components/schemas/") {
					contentType := strings.Replace(requestContent.Schema.Value.Items.Ref, "#/components/schemas/", "", -1)
					if _, ok := swagger.Components.Schemas[contentType]; ok {
						repository, err := api.GetEntityRepository(contentType)
						if err != nil {
							return ctxt, err
						}
						newContext := context.WithValue(ctxt, weoscontext.ENTITY_REPOSITORY, repository)
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
					if _, ok := swagger.Components.Schemas[contentType]; ok {
						repository, err := api.GetEntityRepository(contentType)
						if err != nil {
							return ctxt, err
						}
						newContext := context.WithValue(ctxt, weoscontext.ENTITY_REPOSITORY, repository)
						return newContext, nil
					}
				}
				//use the first schema ref to determine the entity type
				if respContent.Schema.Value.Properties["items"] != nil && respContent.Schema.Value.Properties["items"].Value.Items != nil {
					contentType := strings.Replace(respContent.Schema.Value.Properties["items"].Value.Items.Ref, "#/components/schemas/", "", -1)
					if _, ok := swagger.Components.Schemas[contentType]; ok {
						repository, err := api.GetEntityRepository(contentType)
						if err != nil {
							return ctxt, err
						}
						newContext := context.WithValue(ctxt, weoscontext.ENTITY_REPOSITORY, repository)
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
									if _, ok := swagger.Components.Schemas[contentType]; ok {
										repository, err := api.GetEntityRepository(contentType)
										if err != nil {
											return ctxt, err
										}
										newContext := context.WithValue(ctxt, weoscontext.ENTITY_REPOSITORY, repository)
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

// UserDefinedInitializer adds user defined middleware, controller, command dispatchers and event store to the initialize context
func UserDefinedInitializer(ctxt context.Context, tapi Container, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
	api := tapi.(*RESTAPI)
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
	middlewares := GetOperationMiddlewares(ctxt)
	//if the controller extension is set then add controller to the context
	if middlewareExtension, ok := operation.ExtensionProps.Extensions[MiddlewareExtension]; ok {
		var middlewareNames []string
		err := json.Unmarshal(middlewareExtension.(json.RawMessage), &middlewareNames)
		if err != nil {
			api.EchoInstance().Logger.Errorf("unable to unmarshal middleware '%s'", err)
			return ctxt, fmt.Errorf("middlewares in the specification should be an array of strings on '%s'", path)
		}
		//get the existing middleware from context and then add user defined middleware to it
		for _, middlewareName := range middlewareNames {
			middleware, err := api.GetMiddleware(middlewareName)
			if err != nil {
				return ctxt, fmt.Errorf("unregistered middleware '%s' specified on path '%s'", middlewareName, path)
			}
			middlewares = append(middlewares, middleware)
		}

	}
	ctxt = context.WithValue(ctxt, weoscontext.MIDDLEWARES, middlewares)
	if projectionExtension, ok := operation.ExtensionProps.Extensions[ProjectionExtension]; ok {
		var projectionNames []string
		err := json.Unmarshal(projectionExtension.(json.RawMessage), &projectionNames)
		if err != nil {
			return ctxt, err
		}
		//get the existing middleware from context and then add user defined middleware to it
		definedProjections := GetOperationProjections(ctxt)
		for _, projectionName := range projectionNames {
			projection, err := api.GetProjection(projectionName)
			if err != nil {
				return ctxt, fmt.Errorf("unregistered projection '%s' specified on path '%s'", projectionName, path)
			}
			definedProjections = append(definedProjections, projection)
		}
		ctxt = context.WithValue(ctxt, weoscontext.PROJECTIONS, definedProjections)
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

// StandardInitializer adds standard controller and middleware if not already setup
func StandardInitializer(ctxt context.Context, tapi Container, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
	api := tapi.(*RESTAPI)
	if GetOperationController(ctxt) == nil {
		autoConfigure := false
		handler := ""
		middlewareNames := make(map[string]bool)
		switch strings.ToUpper(method) {
		case "POST":
			if _, ok := pathItem.Post.Extensions["x-command"]; ok {
				handler = "DefaultWriteController"
				autoConfigure = true
				break
			}

			if pathItem.Post.RequestBody == nil {
				api.e.Logger.Warnf("unexpected error: expected request body but got nil")
				break
			} else {
				//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
				for _, value := range pathItem.Post.RequestBody.Value.Content {
					if value.Schema != nil && strings.Contains(value.Schema.Ref, "#/components/schemas/") {
						handler = "DefaultWriteController"
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
							handler = "DefaultWriteController"
							autoConfigure = true
						}

					}

				}
			}
		case "PUT":
			allParam := true
			if _, ok := pathItem.Put.Extensions["x-command"]; ok {
				handler = "DefaultWriteController"
				autoConfigure = true
				break
			}
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
								handler = "DefaultWriteController"
								autoConfigure = true
								break
							}
						} else {
							//if there is no identifiers then id is the default identifier
							for _, param := range pathItem.Put.Parameters {

								if "id" == param.Value.Name {
									handler = "DefaultWriteController"
									autoConfigure = true
									break
								}
								interfaceContext := param.Value.ExtensionProps.Extensions[ContextNameExtension]
								if interfaceContext != nil {
									bytesContext := interfaceContext.(json.RawMessage)
									json.Unmarshal(bytesContext, &contextName)
									if "id" == contextName {
										handler = "DefaultWriteController"
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
			if _, ok := pathItem.Patch.Extensions["x-command"]; ok {
				handler = "DefaultWriteController"
				autoConfigure = true
				break
			}
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
								handler = "DefaultWriteController"
								autoConfigure = true
								break
							}
						} else {
							//if there is no identifiers then id is the default identifier
							for _, param := range pathItem.Patch.Parameters {

								if "id" == param.Value.Name {
									handler = "DefaultWriteController"
									autoConfigure = true
									break
								}
								interfaceContext := param.Value.ExtensionProps.Extensions[ContextNameExtension]
								if interfaceContext != nil {
									bytesContext := interfaceContext.(json.RawMessage)
									json.Unmarshal(bytesContext, &contextName)
									if "id" == contextName {
										handler = "DefaultWriteController"
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
			//assume list controller
			handler = "DefaultListController"
			//if there is a schema in the response then it's probably a view action
			if response := operation.Responses.Get(http.StatusOK); response != nil && response.Value != nil {
				for _, content := range response.Value.Content {
					if tschema := content.Schema; tschema != nil && tschema.Ref != "" {
						handler = "DefaultReadController"
					}
					if response.Value.Extensions["x-templates"] != nil || response.Value.Extensions["x-file"] != nil || response.Value.Extensions["x-folder"] != nil {
						handler = "DefaultReadController"
					}
				}
			}
			autoConfigure = true
		case "DELETE":
			var strContentType string
			allParam := true
			contentTypeExt := pathItem.Delete.ExtensionProps.Extensions[SchemaExtension]
			if _, ok := pathItem.Delete.Extensions["x-command"]; ok {
				handler = "DefaultWriteController"
				autoConfigure = true
				break
			}
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
					handler = "DefaultWriteController"
					autoConfigure = true
					break
				}
			}
			//if there is no identifiers then id is the default identifier
			for _, param := range pathItem.Delete.Parameters {

				if "id" == param.Value.Name {
					handler = "DefaultWriteController"
					autoConfigure = true
					break
				}
				interfaceContext := param.Value.ExtensionProps.Extensions[ContextNameExtension]
				if interfaceContext != nil {
					bytesContext := interfaceContext.(json.RawMessage)
					json.Unmarshal(bytesContext, &contextName)
					if "id" == contextName {
						handler = "DefaultWriteController"
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

// RouteInitializer creates route using information in the initialization context
func RouteInitializer(ctxt context.Context, tapi Container, path string, method string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem, operation *openapi3.Operation) (context.Context, error) {
	var err error

	api := tapi.(*RESTAPI)
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	echoPath := re.ReplaceAllString(path, `:$1`)
	controller := GetOperationController(ctxt)
	repository := GetOperationRepository(ctxt)
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
	entityFactory := GetEntityRepository(ctxt)
	if entityFactory == nil {

	}

	if controller == nil {
		//once no controller is set, the default controller and middleware is added to the path
		controller, err = api.GetController("DefaultReadController")
		if err != nil {
			api.e.Logger.Warnf("unexpected error initializing controller: %s", err)
			return ctxt, fmt.Errorf("controller '%s' set on path '%s' not found", "DefaultResponseController", path)
		}
	}

	//only set up routes if controller is set because echo returns an error if the handler for a route is nil
	if controller != nil {
		var handler echo.HandlerFunc
		handler = controller(api, commandDispatcher, repository, map[string]*openapi3.PathItem{
			path: pathItem,
		}, map[string]*openapi3.Operation{
			method: operation,
		})
		middlewares := GetOperationMiddlewares(ctxt)
		var pathMiddleware []echo.MiddlewareFunc
		for _, tmiddleware := range middlewares {
			//Not sure if CORS middleware and any other middlewares needs to be added
			pathMiddleware = append(pathMiddleware, tmiddleware(api, commandDispatcher, repository, pathItem, operation))
		}
		pathMiddleware = append(pathMiddleware, middleware.CORS())
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
			api.EchoInstance().GET(api.GetWeOSConfig().BasePath+echoPath, handler, pathMiddleware...)
		case "POST":
			api.e.POST(api.GetWeOSConfig().BasePath+echoPath, handler, pathMiddleware...)
		case "PUT":
			api.e.PUT(api.GetWeOSConfig().BasePath+echoPath, handler, pathMiddleware...)
		case "PATCH":
			api.e.PATCH(api.GetWeOSConfig().BasePath+echoPath, handler, pathMiddleware...)
		case "DELETE":
			api.e.DELETE(api.GetWeOSConfig().BasePath+echoPath, handler, pathMiddleware...)
		case "HEAD":
			api.e.HEAD(api.GetWeOSConfig().BasePath+echoPath, handler, pathMiddleware...)
		case "TRACE":
			api.e.TRACE(api.GetWeOSConfig().BasePath+echoPath, handler, pathMiddleware...)
		case "CONNECT":
			api.e.CONNECT(api.GetWeOSConfig().BasePath+echoPath, handler, pathMiddleware...)

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

func GetOperationProjection(ctx context.Context) model.Projection {
	if value, ok := ctx.Value(weoscontext.PROJECTION).(model.Projection); ok {
		return value
	}
	return nil
}

func GetOperationRepository(ctx context.Context) model.EntityRepository {
	if value, ok := ctx.Value(weoscontext.ENTITY_REPOSITORY).(model.EntityRepository); ok {
		return value
	}
	return nil
}

func GetOperationProjections(ctx context.Context) []model.Projection {
	if value, ok := ctx.Value(weoscontext.PROJECTIONS).([]model.Projection); ok {
		return value
	}
	return nil
}

// GetEntityRepository get the configured event factory from the context
func GetEntityRepository(ctx context.Context) model.EntityRepository {
	if value, ok := ctx.Value(weoscontext.ENTITY_REPOSITORY).(model.EntityRepository); ok {
		return value
	}
	return nil
}

// GetSchemaBuilders get a map of the dynamic struct builders for the schemas from the context
func GetSchemaBuilders(ctx context.Context) map[string]ds.Builder {
	if value, ok := ctx.Value(weoscontext.SCHEMA_BUILDERS).(map[string]ds.Builder); ok {
		return value
	}
	return make(map[string]ds.Builder)
}
