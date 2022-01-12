package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4/middleware"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/wepala/weos-service/model"
	"github.com/wepala/weos-service/projections"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
)

//RESTAPI is used to manage the API
type RESTAPI struct {
	*StandardMiddlewares
	*StandardControllers
	Application model.Service
	Log         model.Log
	DB          *sql.DB
	Client      *http.Client
	projection  *projections.GORMProjection
	Config      *APIConfig
	e           *echo.Echo
	PathConfigs map[string]*PathConfig
	Schemas     map[string]interface{}
	middlewares map[string]Middleware
	controllers map[string]Controller
}

type schema struct {
	Name       string
	Type       string
	Ref        string
	Properties []schema
}

//define an interface that all plugins must implement
type APIInterface interface {
	AddPathConfig(path string, config *PathConfig) error
	AddConfig(config *APIConfig) error
	Initialize() error
	EchoInstance() *echo.Echo
	SetEchoInstance(e *echo.Echo)
}

func (p *RESTAPI) AddConfig(config *APIConfig) error {
	p.Config = config
	return nil
}

func (p *RESTAPI) AddPathConfig(path string, config *PathConfig) error {
	if p.PathConfigs == nil {
		p.PathConfigs = make(map[string]*PathConfig)
	}
	p.PathConfigs[path] = config
	return nil
}

func (p *RESTAPI) EchoInstance() *echo.Echo {
	return p.e
}

func (p *RESTAPI) SetEchoInstance(e *echo.Echo) {
	p.e = e
}

func (p *RESTAPI) RegisterMiddleware(name string, middleware Middleware) {
	if p.middlewares == nil {
		p.middlewares = make(map[string]Middleware)
	}
	p.middlewares[name] = middleware
}

func (p *RESTAPI) GetMiddleware(name string) (Middleware, error) {
	//use reflection to check if the middleware is already on the API
	t := reflect.ValueOf(p)
	tmiddleware := t.MethodByName(name)
	//only show error if handler was set
	if tmiddleware.IsValid() {
		return tmiddleware.Interface().(func(model.Service, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) echo.MiddlewareFunc), nil
	}

	if tmiddleware, ok := p.middlewares[name]; ok {
		return tmiddleware, nil
	}

	return nil, fmt.Errorf("middleware '%s' not found", name)
}

func (p *RESTAPI) GetController(name string) (Controller, error) {
	//use reflection to check if the middleware is already on the API
	t := reflect.ValueOf(p)
	tcontroller := t.MethodByName(name)
	//only show error if handler was set
	if tcontroller.IsValid() {
		return tcontroller.Interface().(func(model.Service, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) echo.HandlerFunc), nil
	}

	if tcontroller, ok := p.controllers[name]; ok {
		return tcontroller, nil
	}

	return nil, fmt.Errorf("middleware '%s' not found", name)
}

func (p *RESTAPI) GetSchemas() (map[string]interface{}, error) {
	return p.projection.Schema, nil
}

//Initialize and setup configurations for RESTAPI
func (p *RESTAPI) Initialize() error {
	var err error
	//initialize app
	if p.Client == nil {
		p.Client = &http.Client{
			Timeout: time.Second * 10,
		}
	}
	p.Application, err = model.NewApplicationFromConfig(p.Config.ServiceConfig, p.Log, p.DB, p.Client, nil)
	if err != nil {
		return err
	}

	//enable module
	err = model.Initialize(p.Application)
	if err != nil {
		return err
	}

	//setup projections
	p.projection, err = projections.NewProjection(context.Background(), p.Application, p.Schemas)
	if err != nil {
		return err
	}
	//run fixtures
	err = p.Application.Migrate(context.Background())
	if err != nil {
		return err
	}
	//set log level to debug
	p.EchoInstance().Logger.SetLevel(log.DEBUG)
	return nil
}

//New instantiates and initializes the api
func New(port *string, apiConfig string) {
	e := echo.New()
	var err error
	_, err = Initialize(e, &RESTAPI{}, apiConfig)
	if err != nil {
		e.Logger.Errorf("Unexpected error: '%s'", err)
	}
	e.Logger.Fatal(e.Start(":" + *port))
}

func Initialize(e *echo.Echo, api *RESTAPI, apiConfig string) (*echo.Echo, error) {
	e.HideBanner = true
	if apiConfig == "" {
		apiConfig = "./api.yaml"
	}

	//set echo instance because the instance may not already be in the api that is passed in but the handlers must have access to it
	api.SetEchoInstance(e)

	//configure context middleware using the register method because the context middleware is in it's own file for code readability reasons
	api.RegisterMiddleware("Context", Context)

	var content []byte
	var err error
	//try load file if it's a yaml file otherwise it's the contents of a yaml file WEOS-1009
	if strings.Contains(apiConfig, ".yaml") || strings.Contains(apiConfig, "/yml") {
		content, err = ioutil.ReadFile(apiConfig)
		if err != nil {
			e.Logger.Fatalf("error loading api specification '%s'", err)
		}
	} else {
		content = []byte(apiConfig)
	}

	//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
	tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
	//replace environment variables in file
	tempFile = os.ExpandEnv(string(tempFile))
	tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
	content = []byte(tempFile)
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData(content)
	if err != nil {
		e.Logger.Fatalf("error loading api specification '%s'", err)
	}

	//get the database schema
	api.Schemas = CreateSchema(context.Background(), e, swagger)

	//parse the main config
	var config *APIConfig
	if swagger.ExtensionProps.Extensions[WeOSConfigExtension] != nil {

		data, err := swagger.ExtensionProps.Extensions[WeOSConfigExtension].(json.RawMessage).MarshalJSON()
		if err != nil {
			e.Logger.Fatalf("error loading api config '%s", err)
			return e, err
		}
		err = json.Unmarshal(data, &config)
		if err != nil {
			e.Logger.Fatalf("error loading api config '%s", err)
			return e, err
		}

		err = api.AddConfig(config)
		if err != nil {
			e.Logger.Fatalf("error setting up module '%s", err)
			return e, err
		}

		err = api.Initialize()
		if err != nil {
			e.Logger.Fatalf("error initializing application '%s'", err)
			return e, err
		}

		//setup middleware  - https://echo.labstack.com/middleware/

		//setup global pre middleware
		var preMiddlewares []echo.MiddlewareFunc
		for _, middlewareName := range config.Rest.PreMiddleware {
			t := reflect.ValueOf(middlewareName)
			m := t.MethodByName(middlewareName)
			if !m.IsValid() {
				e.Logger.Fatalf("invalid handler set '%s'", middlewareName)
			}
			preMiddlewares = append(preMiddlewares, m.Interface().(func(handlerFunc echo.HandlerFunc) echo.HandlerFunc))
		}
		//all routes setup after this will use this middleware
		e.Pre(preMiddlewares...)

		//setup global middleware
		var middlewares []echo.MiddlewareFunc
		//prepend Context middleware
		for _, middlewareName := range config.Rest.Middleware {
			tmiddleware, err := api.GetMiddleware(middlewareName)
			if err != nil {
				e.Logger.Fatalf("invalid middleware set '%s'. Must be of type rest.Middleware", middlewareName)
			}
			middlewares = append(middlewares, tmiddleware(api.Application, swagger, nil, nil))
		}
		//all routes setup after this will use this middleware
		e.Use(middlewares...)

	}

	knownActions := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE", "CONNECT"}

	for path, pathData := range swagger.Paths {

		//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
		re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
		echoPath := re.ReplaceAllString(path, `:$1`)
		//prep the middleware by setting up defaults
		allowedOrigins := []string{"*"}
		allowedHeaders := []string{"*"}

		var methodsFound []string
		for _, method := range knownActions {
			//get the operation data
			operationData := pathData.GetOperation(strings.ToUpper(method))
			if operationData != nil {
				methodsFound = append(methodsFound, strings.ToUpper(method))
				operationConfig := &PathConfig{}
				var middlewares []echo.MiddlewareFunc
				contextMiddleware, err := api.GetMiddleware("Context")
				if err != nil {
					return nil, fmt.Errorf("unable to initialize context middleware; confirm that it is registered")
				}

				//get the middleware set on the operation
				middlewareData := operationData.ExtensionProps.Extensions[MiddlewareExtension]
				if middlewareData != nil {
					err = json.Unmarshal(middlewareData.(json.RawMessage), &operationConfig.Middleware)
					if err != nil {
						e.Logger.Fatalf("unable to load middleware on '%s' '%s', error: '%s'", path, method, err)
					}
					for _, middlewareName := range operationConfig.Middleware {
						tmiddleware, err := api.GetMiddleware(middlewareName)
						if err != nil {
							e.Logger.Fatalf("invalid middleware set '%s'. Must be of type rest.Middleware", middlewareName)
						}
						middlewares = append(middlewares, tmiddleware(api.Application, swagger, pathData, operationData))
					}
				}
				middlewares = append(middlewares, contextMiddleware(api.Application, swagger, pathData, operationData))
				controllerData := pathData.GetOperation(strings.ToUpper(method)).ExtensionProps.Extensions[ControllerExtension]
				autoConfigure := false
				if controllerData != nil {
					err = json.Unmarshal(controllerData.(json.RawMessage), &operationConfig.Handler)
					if err != nil {
						e.Logger.Fatalf("unable to load middleware on '%s' '%s', error: '%s'", path, method, err)
					}
				} else {
					switch strings.ToUpper(method) {
					case "POST":
						if pathData.Post.RequestBody == nil {
							e.Logger.Warnf("unexpected error: expected request body but got nil")
							return e, fmt.Errorf("unexpected error: expected request body but got nil")
						}
						//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
						for _, value := range pathData.Post.RequestBody.Value.Content {
							if strings.Contains(value.Schema.Ref, "#/components/schemas/") {
								operationConfig.Handler = "Create"
								autoConfigure = true
							} else if value.Schema.Value.Type == "array" && value.Schema.Value.Items != nil && strings.Contains(value.Schema.Value.Items.Value.Type, "#/components/schemas/") {
								operationConfig.Handler = "CreateBatch"
								autoConfigure = true
							}
						}
					case "PUT":
						allParam := true
						if pathData.Put.RequestBody == nil {
							break
						}
						//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
						for _, value := range pathData.Put.RequestBody.Value.Content {
							if strings.Contains(value.Schema.Ref, "#/components/schemas/") {
								var identifiers []string
								identifierExtension := swagger.Components.Schemas[strings.Replace(value.Schema.Ref, "#/components/schemas/", "", -1)].Value.ExtensionProps.Extensions[IDENTIFIEREXTENSION]
								if identifierExtension != nil {
									bytesId := identifierExtension.(json.RawMessage)
									json.Unmarshal(bytesId, &identifiers)
								}
								var contextName string
								if len(identifiers) == 0 || identifiers == nil {
									for _, param := range pathData.Put.Parameters {
										//check for identifiers,
										//if there is no identifiers then id is the default identifier
										//if there are identifiers then make sure all are listed in parameters

										if "id" == param.Value.Name {
											operationConfig.Handler = "Update"
											autoConfigure = true
											break
										}
										interfaceContext := param.Value.ExtensionProps.Extensions[ContextNameExtension]
										if interfaceContext != nil {
											bytesContext := interfaceContext.(json.RawMessage)
											json.Unmarshal(bytesContext, &contextName)
											if "id" == contextName {
												operationConfig.Handler = "Update"
												autoConfigure = true
												break
											}
										}
									}
								} else {
									for _, identifier := range identifiers {
										//check the parameters
										for _, param := range pathData.Put.Parameters {
											cName := param.Value.ExtensionProps.Extensions[ContextNameExtension]
											if !(identifier == param.Value.Name) || (cName != nil && identifier == cName.(string)) {
												allParam = false
												e.Logger.Warnf("unexpected error: a parameter for each part of the identifier must be set")
												break
											}
										}
									}
									if allParam {
										operationConfig.Handler = "Update"
										autoConfigure = true
									}
								}
							}
						}

					case "PATCH":
						allParam := true
						if pathData.Patch.RequestBody == nil {
							break
						}
						//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
						for _, value := range pathData.Patch.RequestBody.Value.Content {
							if strings.Contains(value.Schema.Ref, "#/components/schemas/") {
								var identifiers []string
								identifierExtension := swagger.Components.Schemas[strings.Replace(value.Schema.Ref, "#/components/schemas/", "", -1)].Value.ExtensionProps.Extensions[IDENTIFIEREXTENSION]
								if identifierExtension != nil {
									bytesId := identifierExtension.(json.RawMessage)
									json.Unmarshal(bytesId, &identifiers)
								}
								var contextName string
								if len(identifiers) == 0 || identifiers == nil {
									for _, param := range pathData.Patch.Parameters {
										//check for identifiers,
										//if there is no identifiers then id is the default identifier
										//if there are identifiers then make sure all are listed in parameters

										if "id" == param.Value.Name {
											operationConfig.Handler = "Update"
											autoConfigure = true
											break
										}
										interfaceContext := param.Value.ExtensionProps.Extensions[ContextNameExtension]
										if interfaceContext != nil {
											bytesContext := interfaceContext.(json.RawMessage)
											json.Unmarshal(bytesContext, &contextName)
											if "id" == contextName {
												operationConfig.Handler = "Update"
												autoConfigure = true
												break
											}
										}
									}
								} else {
									for _, identifier := range identifiers {
										//check the parameters
										for _, param := range pathData.Patch.Parameters {
											cName := param.Value.ExtensionProps.Extensions[ContextNameExtension]
											if !(identifier == param.Value.Name) || (cName != nil && identifier == cName.(string)) {
												allParam = false
												e.Logger.Warnf("unexpected error: a parameter for each part of the identifier must be set")
												break
											}
										}
									}
									if allParam {
										operationConfig.Handler = "Update"
										autoConfigure = true
									}
								}
							}
						}
					}
				}

				if operationConfig.Handler != "" {
					controller, err := api.GetController(operationConfig.Handler)
					handler := controller(api.Application, swagger, pathData, operationData)
					err = api.AddPathConfig(config.BasePath+echoPath, operationConfig)
					if err != nil {
						e.Logger.Fatalf("error adding path config '%s' '%s'", echoPath, err)
					}
					corsMiddleware := middleware.CORSWithConfig(middleware.CORSConfig{
						AllowOrigins: allowedOrigins,
						AllowHeaders: allowedHeaders,
						AllowMethods: methodsFound,
					})
					pathMiddleware := append([]echo.MiddlewareFunc{corsMiddleware}, middlewares...)

					switch method {
					case "GET":
						e.GET(config.BasePath+echoPath, handler, pathMiddleware...)
					case "POST":
						e.POST(config.BasePath+echoPath, handler, pathMiddleware...)
					case "PUT":
						e.PUT(config.BasePath+echoPath, handler, pathMiddleware...)
					case "PATCH":
						e.PATCH(config.BasePath+echoPath, handler, pathMiddleware...)
					case "DELETE":
						e.DELETE(config.BasePath+echoPath, handler, pathMiddleware...)
					case "HEAD":
						e.HEAD(config.BasePath+echoPath, handler, pathMiddleware...)
					case "TRACE":
						e.TRACE(config.BasePath+echoPath, handler, pathMiddleware...)
					case "CONNECT":
						e.CONNECT(config.BasePath+echoPath, handler, pathMiddleware...)

					}

				} else {
					if !autoConfigure {
						//this should not return an error it should log
						e.Logger.Warnf("no handler set, path: '%s' operation '%s'", path, method)
					}
				}
			}
		}
		//setup CORS check on options method
		corsMiddleware := middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins: allowedOrigins,
			AllowHeaders: allowedHeaders,
			AllowMethods: methodsFound,
		})

		e.OPTIONS(config.BasePath+echoPath, func(context echo.Context) error {
			return nil
		}, corsMiddleware)

	}
	return e, nil
}
