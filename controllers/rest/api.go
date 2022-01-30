package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4/middleware"
	"github.com/wepala/weos/projections/dialects"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
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
	Application        model.Service
	Log                model.Log
	DB                 *sql.DB
	Client             *http.Client
	projection         *projections.GORMProjection
	config             *APIConfig
	e                  *echo.Echo
	PathConfigs        map[string]*PathConfig
	Schemas            map[string]ds.Builder
	Swagger            *openapi3.Swagger
	middlewares        map[string]Middleware
	controllers        map[string]Controller
	eventStores        map[string]model.EventRepository
	commandDispatchers map[string]model.CommandDispatcher
	projections        map[string]projections.Projection
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
	Initialize(ctxt context.Context) error
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

//RegisterEventStore Add event store so that it can be referenced in the OpenAPI spec
func (p *RESTAPI) RegisterEventStore(name string, dispatcher model.EventRepository) {
	if p.eventStores == nil {
		p.eventStores = make(map[string]model.EventRepository)
	}
	p.eventStores[name] = dispatcher
}

//RegisterCommandDispatcher Add command dispatcher so that it can be referenced in the OpenAPI spec
func (p *RESTAPI) RegisterCommandDispatcher(name string, dispatcher model.CommandDispatcher) {
	if p.commandDispatchers == nil {
		p.commandDispatchers = make(map[string]model.CommandDispatcher)
	}
	p.commandDispatchers[name] = dispatcher
}

//RegisterProjection Add command dispatcher so that it can be referenced in the OpenAPI spec
func (p *RESTAPI) RegisterProjection(name string, projection projections.Projection) {
	if p.projections == nil {
		p.projections = make(map[string]projections.Projection)
	}
	p.projections[name] = projection
}

//GetMiddleware get middleware by name
func (p *RESTAPI) GetMiddleware(name string) (Middleware, error) {
	if tmiddleware, ok := p.middlewares[name]; ok {
		return tmiddleware, nil
	}

	//use reflection to check if the middleware is already on the API
	t := reflect.ValueOf(p)
	tmiddleware := t.MethodByName(name)
	//only show error if handler was set
	if tmiddleware.IsValid() {
		return tmiddleware.Interface().(func(model.Service, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) echo.MiddlewareFunc), nil
	}

	return nil, fmt.Errorf("middleware '%s' not found", name)
}

//GetController get controller by name
func (p *RESTAPI) GetController(name string) (Controller, error) {
	if tcontroller, ok := p.controllers[name]; ok {
		return tcontroller, nil
	}

	//use reflection to check if the middleware is already on the API
	t := reflect.ValueOf(p)
	tcontroller := t.MethodByName(name)
	//only show error if handler was set
	if tcontroller.IsValid() {
		return tcontroller.Interface().(func(model.Service, *openapi3.Swagger, *openapi3.PathItem, *openapi3.Operation) echo.HandlerFunc), nil
	}

	return nil, fmt.Errorf("controller '%s' not found", name)
}

//GetEventStore get event dispatcher by name
func (p *RESTAPI) GetEventStore(name string) (model.EventRepository, error) {
	if tdispatcher, ok := p.eventStores[name]; ok {
		return tdispatcher, nil
	}
	return nil, fmt.Errorf("event disptacher '%s' not found", name)
}

//GetCommandDispatcher get event dispatcher by name
func (p *RESTAPI) GetCommandDispatcher(name string) (model.CommandDispatcher, error) {
	if tdispatcher, ok := p.commandDispatchers[name]; ok {
		return tdispatcher, nil
	}
	return nil, fmt.Errorf("command disptacher '%s' not found", name)
}

//GetProjection get event dispatcher by name
func (p *RESTAPI) GetProjection(name string) (projections.Projection, error) {
	if tdispatcher, ok := p.projections[name]; ok {
		return tdispatcher, nil
	}
	return nil, fmt.Errorf("projection '%s' not found", name)
}

func (p *RESTAPI) GetSchemas() (map[string]interface{}, error) {
	schemes := map[string]interface{}{}
	for name, s := range p.Schemas {
		schemes[name] = s.Build().New()
	}
	return schemes, nil
}

//Initialize and setup configurations for RESTAPI
func (p *RESTAPI) Initialize(ctxt context.Context) error {
	if p.config != nil && p.config.Database != nil {
		//setup default projection
		var gormDB *gorm.DB
		var err error

		p.DB, gormDB, err = p.SQLConnectionFromConfig(p.config.Database)
		if err != nil {
			return err
		}

		//setup default projection if gormDB is configured
		if gormDB != nil {
			defaultProjection, err := projections.NewProjection(ctxt, gormDB, p.EchoInstance().Logger)
			if err != nil {
				return err
			}
			p.RegisterProjection("Default", defaultProjection)
			//get the database schema
			schemas := CreateSchema(context.Background(), p.EchoInstance(), p.Swagger)
			err = defaultProjection.Migrate(ctxt, schemas)
			if err != nil {
				return err
			}
		}
	}
	//setup default event store if there isn't already one
	if _, err := p.GetEventStore("Default"); err != nil {
		//if there is a projection then add the event handler as a subscriber to the event store
		if defaultProjection, err := p.GetProjection("Default"); err == nil {
			//only setup the gorm event repository if it's a gorm projection
			if gormProjection, ok := defaultProjection.(*projections.GORMProjection); ok {
				defaultEventStore, err := model.NewBasicEventRepository(gormProjection.DB(), p.EchoInstance().Logger, false, "", "")
				if err != nil {
					return err
				}
				defaultEventStore.AddSubscriber(defaultProjection.GetEventHandler())
				p.RegisterEventStore("Default", defaultEventStore)
			}
		}
	}

	//setup command dispatcher
	if _, err := p.GetCommandDispatcher("Default"); err != nil {
		defaultCommandDispatcher := &model.DefaultCommandDispatcher{}
		//setup default commands
		defaultCommandDispatcher.AddSubscriber(model.Create(context.Background(), nil, "", ""), model.CreateHandler)
		//defaultCommandDispatcher.AddSubscriber(model.CreateBatch(context.Background(), nil, ""), receiver.CreateBatch)
		//defaultCommandDispatcher.AddSubscriber(model.Update(context.Background(), nil, ""), receiver.Update)
	}

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

	//initialize app
	if p.Client == nil {
		p.Client = &http.Client{
			Timeout: time.Second * 10,
		}
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

//SQLConnectionFromConfig get db connection based on a config
func (p *RESTAPI) SQLConnectionFromConfig(config *model.DBConfig) (*sql.DB, *gorm.DB, error) {
	var connStr string
	var err error

	switch config.Driver {
	case "sqlite3":
		//check if file exists and if not create it. We only do this if a memory only db is NOT asked for
		//(Note that if it's a combination we go ahead and create the file) https://www.sqlite.org/inmemorydb.html
		if config.Database != ":memory:" {
			if _, err = os.Stat(config.Database); os.IsNotExist(err) {
				_, err = os.Create(strings.Replace(config.Database, ":memory:", "", -1))
				if err != nil {
					return nil, nil, model.NewError(fmt.Sprintf("error creating sqlite database '%s'", config.Database), err)
				}
			}
		}

		connStr = fmt.Sprintf("%s",
			config.Database)

		//update connection string to include authentication IF a username is set
		if config.User != "" {
			authenticationString := fmt.Sprintf("?_auth&_auth_user=%s&_auth_pass=%s&_auth_crypt=sha512&_foreign_keys=on",
				config.User, config.Password)
			connStr = connStr + authenticationString
		} else {
			connStr = connStr + "?_foreign_keys=on"
		}
		log.Debugf("sqlite connection string '%s'", connStr)
	case "sqlserver":
		connStr = fmt.Sprintf("sqlserver://%s:%s@%s:%s/%s",
			config.User, config.Password, config.Host, strconv.Itoa(config.Port), config.Database)
	case "ramsql":
		connStr = "Testing"
	case "mysql":
		connStr = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?sql_mode='ERROR_FOR_DIVISION_BY_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION'&parseTime=true",
			config.User, config.Password, config.Host, strconv.Itoa(config.Port), config.Database)
	case "clickhouse":
		connStr = fmt.Sprintf("tcp://%s:%s?username=%s&password=%s&database=%s",
			config.Host, strconv.Itoa(config.Port), config.User, config.Password, config.Database)
	case "postgres":
		connStr = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			config.Host, strconv.Itoa(config.Port), config.User, config.Password, config.Database)
	default:
		return nil, nil, errors.New(fmt.Sprintf("db driver '%s' is not supported ", config.Driver))
	}

	db, err := sql.Open(config.Driver, connStr)
	if err != nil {
		return nil, nil, errors.New(fmt.Sprintf("error setting up connection to database '%s' with connection '%s'", err, connStr))
	}

	db.SetMaxOpenConns(config.MaxOpen)
	db.SetMaxIdleConns(config.MaxIdle)

	//setup gorm
	var gormDB *gorm.DB
	switch config.Driver {
	case "postgres":
		gormDB, err = gorm.Open(dialects.NewPostgres(postgres.Config{
			Conn: db,
		}), nil)
		if err != nil {
			return nil, nil, err
		}
	case "sqlite3":
		gormDB, err = gorm.Open(&dialects.SQLite{
			sqlite.Dialector{
				Conn: db,
			},
		}, nil)
		if err != nil {
			return nil, nil, err
		}
	case "mysql":
		gormDB, err = gorm.Open(dialects.NewMySQL(mysql.Config{
			Conn: db,
		}), nil)
		if err != nil {
			return nil, nil, err
		}
	case "ramsql": //this is for testing
		gormDB = &gorm.DB{}
	case "sqlserver":
		gormDB, err = gorm.Open(sqlserver.New(sqlserver.Config{
			Conn: db,
		}), nil)
		if err != nil {
			return nil, nil, err
		}
	case "clickhouse":
		gormDB, err = gorm.Open(clickhouse.New(clickhouse.Config{
			Conn: db,
		}), nil)
		if err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, errors.New(fmt.Sprintf("we don't support database driver '%s'", config.Driver))
	}
	return db, gormDB, err
}
