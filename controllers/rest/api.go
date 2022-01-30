package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/labstack/echo/v4/middleware"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
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
	config      *APIConfig
	e           *echo.Echo
	PathConfigs map[string]*PathConfig
	Schemas     map[string]ds.Builder
	Swagger     *openapi3.Swagger
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
	p.config = config
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

//RegisterMiddleware Add middleware so that it can be referenced in the OpenAPI spec
func (p *RESTAPI) RegisterMiddleware(name string, middleware Middleware) {
	if p.middlewares == nil {
		p.middlewares = make(map[string]Middleware)
	}
	p.middlewares[name] = middleware
}

//RegisterController Add controller so that it can be referenced in the OpenAPI spec
func (p *RESTAPI) RegisterController(name string, controller Controller) {
	if p.controllers == nil {
		p.controllers = make(map[string]Controller)
	}
	p.controllers[name] = controller
}

//GetMiddleware get middleware by name
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

//GetController get controller by name
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

	return nil, fmt.Errorf("controller '%s' not found", name)
}

func (p *RESTAPI) GetSchemas() (map[string]interface{}, error) {
	schemes := map[string]interface{}{}
	for name, s := range p.Schemas {
		schemes[name] = s.Build().New()
	}
	return schemes, nil
}

//Initialize and setup configurations for RESTAPI
func (p *RESTAPI) Initialize() error {

	//setup middleware  - https://echo.labstack.com/middleware/

	//setup global pre middleware
	var preMiddlewares []echo.MiddlewareFunc
	for _, middlewareName := range p.config.Rest.PreMiddleware {
		t := reflect.ValueOf(middlewareName)
		m := t.MethodByName(middlewareName)
		if !m.IsValid() {
			p.e.Logger.Fatalf("invalid handler set '%s'", middlewareName)
		}
		preMiddlewares = append(preMiddlewares, m.Interface().(func(handlerFunc echo.HandlerFunc) echo.HandlerFunc))
	}
	//all routes setup after this will use this middleware
	p.e.Pre(preMiddlewares...)

	//setup global middleware
	var middlewares []echo.MiddlewareFunc
	//prepend Context middleware
	for _, middlewareName := range p.config.Rest.Middleware {
		tmiddleware, err := p.GetMiddleware(middlewareName)
		if err != nil {
			p.e.Logger.Fatalf("invalid middleware set '%s'. Must be of type rest.Middleware", middlewareName)
		}
		middlewares = append(middlewares, tmiddleware(p.Application, p.Swagger, nil, nil))
	}
	//all routes setup after this will use this middleware
	p.e.Use(middlewares...)

	var err error
	//initialize app
	if p.Client == nil {
		p.Client = &http.Client{
			Timeout: time.Second * 10,
		}
	}
	p.Application, err = model.NewApplicationFromConfig(p.config.ServiceConfig, p.Log, p.DB, p.Client, nil)
	if err != nil {
		return err
	}

	//setup projections
	p.projection, err = projections.NewProjection(context.Background(), p.Application, p.Schemas)
	if err != nil {
		return err
	}

	//enable module
	err = model.Initialize(p.Application)
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

	//setup routes
	knownActions := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE", "CONNECT"}

	for path, pathData := range p.Swagger.Paths {

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
				contextMiddleware, err := p.GetMiddleware("Context")
				if err != nil {
					return fmt.Errorf("unable to initialize context middleware; confirm that it is registered")
				}

				//get the middleware set on the operation
				middlewareData := operationData.ExtensionProps.Extensions[MiddlewareExtension]
				if middlewareData != nil {
					err = json.Unmarshal(middlewareData.(json.RawMessage), &operationConfig.Middleware)
					if err != nil {
						p.e.Logger.Fatalf("unable to load middleware on '%s' '%s', error: '%s'", path, method, err)
					}
					for _, middlewareName := range operationConfig.Middleware {
						tmiddleware, err := p.GetMiddleware(middlewareName)
						if err != nil {
							p.e.Logger.Fatalf("invalid middleware set '%s'. Must be of type rest.Middleware", middlewareName)
						}
						middlewares = append(middlewares, tmiddleware(p.Application, p.Swagger, pathData, operationData))
					}
				}
				middlewares = append(middlewares, contextMiddleware(p.Application, p.Swagger, pathData, operationData))
				controllerData := pathData.GetOperation(strings.ToUpper(method)).ExtensionProps.Extensions[ControllerExtension]
				autoConfigure := false
				if controllerData != nil {
					err = json.Unmarshal(controllerData.(json.RawMessage), &operationConfig.Handler)
					if err != nil {
						p.e.Logger.Fatalf("unable to load middleware on '%s' '%s', error: '%s'", path, method, err)
					}
					//checks if the controller explicitly stated and whether the endpoint is valid
					if strings.ToUpper(method) == "GET" {
						if operationConfig.Handler == "List" {
							if pathData.Get.Responses != nil && pathData.Get.Responses["200"].Value.Content != nil {
								for _, val := range pathData.Get.Responses["200"].Value.Content {
									//checks if the response refers to an array schema
									if val.Schema.Value.Properties != nil && val.Schema.Value.Properties["items"] != nil && val.Schema.Value.Properties["items"].Value.Type == "array" && val.Schema.Value.Properties["items"].Value.Items != nil && strings.Contains(val.Schema.Value.Properties["items"].Value.Items.Ref, "#/components/schemas/") {
										autoConfigure = true
										break
									}
								}
							}
							if !autoConfigure {
								operationConfig.Handler = ""
							}
						}
					}

				} else {
					//Adds standard controller to path
					autoConfigure, err = AddStandardController(p.e, pathData, method, p.Swagger, operationConfig)
					if err != nil {
						return err
					}
				}

				if operationConfig.Handler != "" {
					controller, err := p.GetController(operationConfig.Handler)
					if err != nil {
						p.e.Logger.Fatalf("error getting controller '%s'", err)
						return err
					}
					handler := controller(p.Application, p.Swagger, pathData, operationData)
					err = p.AddPathConfig(p.config.BasePath+echoPath, operationConfig)
					if err != nil {
						p.e.Logger.Fatalf("error adding path config '%s' '%s'", echoPath, err)
					}
					corsMiddleware := middleware.CORSWithConfig(middleware.CORSConfig{
						AllowOrigins: allowedOrigins,
						AllowHeaders: allowedHeaders,
						AllowMethods: methodsFound,
					})
					pathMiddleware := append([]echo.MiddlewareFunc{corsMiddleware}, middlewares...)

					switch method {
					case "GET":
						p.e.GET(p.config.BasePath+echoPath, handler, pathMiddleware...)
					case "POST":
						p.e.POST(p.config.BasePath+echoPath, handler, pathMiddleware...)
					case "PUT":
						p.e.PUT(p.config.BasePath+echoPath, handler, pathMiddleware...)
					case "PATCH":
						p.e.PATCH(p.config.BasePath+echoPath, handler, pathMiddleware...)
					case "DELETE":
						p.e.DELETE(p.config.BasePath+echoPath, handler, pathMiddleware...)
					case "HEAD":
						p.e.HEAD(p.config.BasePath+echoPath, handler, pathMiddleware...)
					case "TRACE":
						p.e.TRACE(p.config.BasePath+echoPath, handler, pathMiddleware...)
					case "CONNECT":
						p.e.CONNECT(p.config.BasePath+echoPath, handler, pathMiddleware...)

					}

				} else {
					if !autoConfigure {
						//this should not return an error it should log
						p.e.Logger.Warnf("no handler set, path: '%s' operation '%s'", path, method)
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

		p.e.OPTIONS(p.config.BasePath+echoPath, func(context echo.Context) error {
			return nil
		}, corsMiddleware)

	}
	return nil
}

//New instantiates and initializes the api
func New(apiConfig string) (*RESTAPI, error) {
	e := echo.New()
	var err error
	api := &RESTAPI{}
	_, err = Initialize(e, api, apiConfig)
	if err != nil {
		e.Logger.Errorf("Unexpected error: '%s'", err)
	}
	return api, err
}

//Start API
func Start(port string, apiConfig string) *RESTAPI {
	api, err := New(apiConfig)
	if err != nil {
		api.EchoInstance().Logger.Error(err)
	}
	err = api.Initialize()
	if err != nil {
		api.EchoInstance().Logger.Error(err)
	}
	api.EchoInstance().Logger.Fatal(api.EchoInstance().Start(":" + port))
	return api
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
	}
	api.Swagger = swagger
	return e, nil
}
