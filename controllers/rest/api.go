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
	"github.com/wepala/weos-service/model"
	"github.com/wepala/weos-service/projections"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
)

//RESTAPI is used to manage the API
type RESTAPI struct {
	*StandardMiddleware
	Application model.Service
	Log         model.Log
	DB          *sql.DB
	Client      *http.Client
	projection  *projections.GORMProjection
	Config      *APIConfig
	e           *echo.Echo
	PathConfigs map[string]*PathConfig
	Schemas     map[string]*openapi3.SchemaRef
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
	// err = module.Initialize(a.Service)
	// if err != nil {
	// 	return err
	// }

	s := projections.Service{}
	structs, err := s.CreateSchema(context.Background(), p.Schemas)
	if err != nil {
		return err
	}
	for name, s := range structs {
		fmt.Printf("struct %s: %v", name, s)
	}

	//setup projections
	p.projection, err = projections.NewProjection(structs, p.Application)
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
	Initialize(e, &RESTAPI{}, apiConfig)
	e.Logger.Fatal(e.Start(":" + *port))
}

func Initialize(e *echo.Echo, api *RESTAPI, apiConfig string) *echo.Echo {
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
	api.Schemas = swagger.Components.Schemas

	//parse the main config
	var config *APIConfig
	if swagger.ExtensionProps.Extensions[WeOSConfigExtension] != nil {

		data, err := swagger.ExtensionProps.Extensions[WeOSConfigExtension].(json.RawMessage).MarshalJSON()
		if err != nil {
			e.Logger.Fatalf("error loading api config '%s", err)
			return e
		}
		err = json.Unmarshal(data, &config)
		if err != nil {
			e.Logger.Fatalf("error loading api config '%s", err)
			return e
		}

		err = api.AddConfig(config)
		if err != nil {
			e.Logger.Fatalf("error setting up module '%s", err)
			return e
		}

		err = api.Initialize()
		if err != nil {
			e.Logger.Fatalf("error initializing application '%s'", err)
			return e
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
		config.Rest.Middleware = append([]string{"Context"}, config.Rest.Middleware...)
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
				//get the middleware set on the operation
				middlewareData := operationData.ExtensionProps.Extensions[MiddlewareExtension]
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

				operationConfigData := pathData.GetOperation(strings.ToUpper(method)).ExtensionProps.Extensions[WeOSConfigExtension]
				if operationConfigData != nil {
					bytes, err := operationConfigData.(json.RawMessage).MarshalJSON()
					if err != nil {
						e.Logger.Fatalf("error reading model config on the path '%s' '%s'", path, err)
					}

					if err = json.Unmarshal(bytes, &operationConfig); err != nil {
						e.Logger.Fatalf("error reading model config on the path '%s' '%s'", path, err)
						return e
					}

					t := reflect.ValueOf(api)
					handler := t.MethodByName(operationConfig.Handler)
					//only show error if handler was set
					if operationConfig.Handler != "" && !handler.IsValid() {
						e.Logger.Fatalf("invalid handler set '%s'", operationConfig.Handler)
					}

					if operationConfig.Group { //TODO move this form here because it creates weird behavior
						group := e.Group(config.BasePath + path)
						err = api.AddPathConfig(config.BasePath+path, operationConfig)
						if err != nil {
							e.Logger.Fatalf("error adding path config '%s' '%s'", config.BasePath+path, err)
						}
						group.Use(middlewares...)
					} else {
						//TODO make it so that it automatically matches the paths to a group based on the prefix

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
							e.GET(config.BasePath+echoPath, handler.Interface().(func(ctx echo.Context) error), pathMiddleware...)
						case "POST":
							e.POST(config.BasePath+echoPath, handler.Interface().(func(ctx echo.Context) error), pathMiddleware...)
						case "PUT":
							e.PUT(config.BasePath+echoPath, handler.Interface().(func(ctx echo.Context) error), pathMiddleware...)
						case "PATCH":
							e.PATCH(config.BasePath+echoPath, handler.Interface().(func(ctx echo.Context) error), pathMiddleware...)
						case "DELETE":
							e.DELETE(config.BasePath+echoPath, handler.Interface().(func(ctx echo.Context) error), pathMiddleware...)
						case "HEAD":
							e.HEAD(config.BasePath+echoPath, handler.Interface().(func(ctx echo.Context) error), pathMiddleware...)
						case "TRACE":
							e.TRACE(config.BasePath+echoPath, handler.Interface().(func(ctx echo.Context) error), pathMiddleware...)
						case "CONNECT":
							e.CONNECT(config.BasePath+echoPath, handler.Interface().(func(ctx echo.Context) error), pathMiddleware...)

						}
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
	return e
}
