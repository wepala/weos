package rest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/casbin/casbin/v2"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rakyll/statik/fs"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/projections/dialects"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
)

var InvalidAWSDriver = errors.New("invalid aws driver specified, must be postgres or mysql")

//RESTAPI is used to manage the API
type RESTAPI struct {
	Application                    model.Service
	Log                            model.Log
	DB                             *sql.DB
	projection                     *projections.GORMDB
	Config                         *APIConfig
	securityConfiguration          *SecurityConfiguration
	e                              *echo.Echo
	PathConfigs                    map[string]*PathConfig
	Schemas                        map[string]ds.Builder
	Swagger                        *openapi3.Swagger
	middlewares                    map[string]Middleware
	controllers                    map[string]Controller
	eventStores                    map[string]model.EventRepository
	commandDispatchers             map[string]model.CommandDispatcher
	projections                    map[string]model.Projection
	logs                           map[string]model.Log
	httpClients                    map[string]*http.Client
	globalInitializers             []GlobalInitializer
	operationInitializers          []OperationInitializer
	registeredInitializers         map[string]int
	prePathInitializers            []PathInitializer
	registeredPrePathInitializers  map[reflect.Value]int
	postPathInitializers           []PathInitializer
	registeredPostPathInitializers map[reflect.Value]int
	entityFactories                map[string]model.EntityFactory
	dbConnections                  map[string]*sql.DB
	gormConnections                map[string]*gorm.DB
	gormConnection                 *gorm.DB
	enforcers                      map[string]*casbin.Enforcer
	entityRepositories             map[string]model.EntityRepository
}

//define an interface that all plugins must implement
type APIInterface interface {
	AddPathConfig(path string, config *PathConfig) error
	AddConfig(config *APIConfig) error
	Initialize(ctxt context.Context) error
	EchoInstance() *echo.Echo
	SetEchoInstance(e *echo.Echo)
}

//Deprecated: 02/13/2022 made Config public
func (p *RESTAPI) AddConfig(config *APIConfig) error {
	p.Config = config
	return nil
}

//Deprecated: 02/13/2022 This should not but actively used
//AddPathConfig add path Config
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
func (p *RESTAPI) RegisterEventStore(name string, repository model.EventRepository) {
	if p.eventStores == nil {
		p.eventStores = make(map[string]model.EventRepository)
	}
	p.eventStores[name] = repository
}

// RegisterGlobalInitializer  add global initializer if it's not already there
// Deprecated: Use RegisterInitializer instead
func (p *RESTAPI) RegisterGlobalInitializer(initializer GlobalInitializer) {
	if p.registeredInitializers == nil {
		p.registeredInitializers = make(map[string]int)
	}
	//only add initializer if it doesn't already exist
	tpoint := reflect.ValueOf(initializer)
	functionName := runtime.FuncForPC(tpoint.Pointer()).Name()
	if _, ok := p.registeredInitializers[functionName]; !ok {
		p.globalInitializers = append(p.globalInitializers, initializer)
		p.registeredInitializers[functionName] = len(p.globalInitializers)
	}
}

func (p *RESTAPI) RegisterInitializer(key string, initializer GlobalInitializer) {
	if p.registeredInitializers == nil {
		p.registeredInitializers = make(map[string]int)
	}
	if _, ok := p.registeredInitializers[key]; !ok {
		p.globalInitializers = append(p.globalInitializers, initializer)
		p.registeredInitializers[key] = len(p.globalInitializers)
	}
}

//RegisterOperationInitializer add operation initializer if it's not already there
func (p *RESTAPI) RegisterOperationInitializer(initializer OperationInitializer) {
	if p.registeredInitializers == nil {
		p.registeredInitializers = make(map[string]int)
	}
	//only add initializer if it doesn't already exist
	tpoint := reflect.ValueOf(initializer)
	functionName := runtime.FuncForPC(tpoint.Pointer()).Name()
	if _, ok := p.registeredInitializers[functionName]; !ok {
		p.operationInitializers = append(p.operationInitializers, initializer)
		p.registeredInitializers[functionName] = len(p.operationInitializers)
	}

}

//RegisterPrePathInitializer add path initializer that runs BEFORE operation initializers if it's not already there
func (p *RESTAPI) RegisterPrePathInitializer(initializer PathInitializer) {
	if p.registeredPrePathInitializers == nil {
		p.registeredPrePathInitializers = make(map[reflect.Value]int)
	}
	//only add initializer if it doesn't already exist
	tpoint := reflect.ValueOf(initializer)
	if _, ok := p.registeredPrePathInitializers[tpoint]; !ok {
		p.prePathInitializers = append(p.prePathInitializers, initializer)
		p.registeredPrePathInitializers[tpoint] = len(p.prePathInitializers)
	}

}

//RegisterPostPathInitializer add path initializer that runs AFTER operation initializers if it's not already there
func (p *RESTAPI) RegisterPostPathInitializer(initializer PathInitializer) {
	if p.registeredPostPathInitializers == nil {
		p.registeredPostPathInitializers = make(map[reflect.Value]int)
	}
	//only add initializer if it doesn't already exist
	tpoint := reflect.ValueOf(initializer)
	if _, ok := p.registeredPostPathInitializers[tpoint]; !ok {
		p.postPathInitializers = append(p.postPathInitializers, initializer)
		p.registeredPostPathInitializers[tpoint] = len(p.postPathInitializers)
	}

}

//RegisterCommandDispatcher Add command dispatcher so that it can be referenced in the OpenAPI spec
func (p *RESTAPI) RegisterCommandDispatcher(name string, dispatcher model.CommandDispatcher) {
	if p.commandDispatchers == nil {
		p.commandDispatchers = make(map[string]model.CommandDispatcher)
	}
	p.commandDispatchers[name] = dispatcher
}

//RegisterProjection Add command dispatcher so that it can be referenced in the OpenAPI spec
func (p *RESTAPI) RegisterProjection(name string, projection model.Projection) {
	if p.projections == nil {
		p.projections = make(map[string]model.Projection)
	}
	p.projections[name] = projection
}

//RegisterEntityFactory Adds entity factory so that it can be referenced in the OpenAPI spec
func (p *RESTAPI) RegisterEntityFactory(name string, factory model.EntityFactory) {
	if p.entityFactories == nil {
		p.entityFactories = make(map[string]model.EntityFactory)
	}
	p.entityFactories[name] = factory
}

//RegisterDBConnection save db connection
func (p *RESTAPI) RegisterDBConnection(name string, connection *sql.DB) {
	if p.dbConnections == nil {
		p.dbConnections = make(map[string]*sql.DB)
	}
	p.dbConnections[name] = connection
}

//RegisterGORMDB save gorm connection
func (p *RESTAPI) RegisterGORMDB(name string, connection *gorm.DB) {
	p.gormConnection = connection
}

func (p *RESTAPI) RegisterPermissionEnforcer(name string, enforcer *casbin.Enforcer) {
	if p.enforcers == nil {
		p.enforcers = make(map[string]*casbin.Enforcer)
	}
	p.enforcers[name] = enforcer
}

func (p *RESTAPI) GetPermissionEnforcer(name string) (*casbin.Enforcer, error) {
	if tenforcer, ok := p.enforcers[name]; ok {
		return tenforcer, nil
	}
	return nil, fmt.Errorf("permission enforcer '%s' not found", name)
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
		return tmiddleware.Interface().(func(api Container, commandDispatcher model.CommandDispatcher, repository model.EntityRepository, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc), nil
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
		return tcontroller.Interface().(func(api Container, commandDispatcher model.CommandDispatcher, repository model.EntityRepository, pathMap map[string]*openapi3.PathItem, operation map[string]*openapi3.Operation) echo.HandlerFunc), nil
	}

	return nil, fmt.Errorf("controller '%s' not found", name)
}

//GetEventStore get event dispatcher by name
func (p *RESTAPI) GetEventStore(name string) (model.EventRepository, error) {
	if repository, ok := p.eventStores[name]; ok {
		return repository, nil
	}
	return nil, fmt.Errorf("event repository '%s' not found", name)
}

//GetCommandDispatcher get event dispatcher by name
func (p *RESTAPI) GetCommandDispatcher(name string) (model.CommandDispatcher, error) {
	if tdispatcher, ok := p.commandDispatchers[name]; ok {
		return tdispatcher, nil
	}
	return nil, fmt.Errorf("command disptacher '%s' not found", name)
}

//GetProjection get event dispatcher by name
func (p *RESTAPI) GetProjection(name string) (model.Projection, error) {
	if tdispatcher, ok := p.projections[name]; ok {
		return tdispatcher, nil
	}
	return nil, fmt.Errorf("projection '%s' not found", name)
}

func (p *RESTAPI) RegisterEntityRepository(name string, repository model.EntityRepository) {
	if p.entityRepositories == nil {
		p.entityRepositories = make(map[string]model.EntityRepository)
	}
	p.entityRepositories[name] = repository
}

func (p *RESTAPI) GetEntityRepository(name string) (model.EntityRepository, error) {
	if trepository, ok := p.entityRepositories[name]; ok {
		return trepository, nil
	}
	return nil, fmt.Errorf("entity repository '%s' not found", name)
}

//GetGlobalInitializers get global intializers in the order they were registered
func (p *RESTAPI) GetGlobalInitializers() []GlobalInitializer {
	return p.globalInitializers
}

//GetOperationInitializers get operation intializers in the order they were registered
func (p *RESTAPI) GetOperationInitializers() []OperationInitializer {
	return p.operationInitializers
}

//GetPrePathInitializers get path intializers in the order they were registered that run BEFORE the operations are processed
func (p *RESTAPI) GetPrePathInitializers() []PathInitializer {
	return p.prePathInitializers
}

//GetPostPathInitializers get path intializers in the order they were registered that run AFTER the operations are processed
func (p *RESTAPI) GetPostPathInitializers() []PathInitializer {
	return p.postPathInitializers
}

func (p *RESTAPI) GetSchemas() (map[string]interface{}, error) {
	schemes := map[string]interface{}{}
	for name, s := range p.Schemas {
		schemes[name] = s.Build().New()
	}
	return schemes, nil
}

//GetEntityFactories get event factories
func (p *RESTAPI) GetEntityFactories() map[string]model.EntityFactory {
	return p.entityFactories
}

func (p *RESTAPI) GetEntityFactory(name string) (model.EntityFactory, error) {
	if entityFactory, ok := p.entityFactories[name]; ok {
		return entityFactory, nil
	}
	return nil, fmt.Errorf("entity factory '%s' not found", name)
}

//GetDBConnection get db connection by name
func (p *RESTAPI) GetDBConnection(name string) (*sql.DB, error) {
	if tconnection, ok := p.dbConnections[name]; ok {
		return tconnection, nil
	}
	return nil, fmt.Errorf("database connection '%s' not found", name)
}

//GetGormDBConnection get gorm connection by name
func (p *RESTAPI) GetGormDBConnection(name string) (*gorm.DB, error) {
	return p.gormConnection, nil
}

func (p *RESTAPI) GetConfig() *openapi3.Swagger {
	return p.Swagger
}

func (p *RESTAPI) GetWeOSConfig() *APIConfig {
	return p.Config
}

//RegisterLog setup a log
func (p *RESTAPI) RegisterLog(name string, logger model.Log) {
	if p.logs == nil {
		p.logs = make(map[string]model.Log)
	}
	p.logs[name] = logger
}

func (p *RESTAPI) GetLog(name string) (model.Log, error) {
	if tlog, ok := p.logs[name]; ok {
		return tlog, nil
	}
	return nil, fmt.Errorf("log '%s' not found", name)
}

func (p *RESTAPI) RegisterHTTPClient(name string, client *http.Client) {
	if p.httpClients == nil {
		p.httpClients = make(map[string]*http.Client)
	}
	p.httpClients[name] = client
}

func (p *RESTAPI) GetHTTPClient(name string) (*http.Client, error) {
	if client, ok := p.httpClients[name]; ok {
		return client, nil
	}
	return nil, fmt.Errorf("http client '%s' not found", name)
}

func (p *RESTAPI) RegisterSecurityConfiguration(configuration *SecurityConfiguration) {
	p.securityConfiguration = configuration
}

func (p *RESTAPI) GetSecurityConfiguration() *SecurityConfiguration {
	return p.securityConfiguration
}

const SWAGGERUIENDPOINT = "/_discover/"
const SWAGGERJSONENDPOINT = "/_discover_json"

//RegisterSwaggerAPI creates default swagger api from binary
func (p *RESTAPI) RegisterDefaultSwaggerAPI(pathMiddleware []echo.MiddlewareFunc) error {
	statikFS, err := fs.New()
	if err != nil {
		return NewControllerError("Got an error formatting response", err, http.StatusInternalServerError)
	}
	static := http.FileServer(statikFS)
	sh := http.StripPrefix(p.Config.BasePath+SWAGGERUIENDPOINT, static)
	handler := echo.WrapHandler(sh)
	p.e.GET(p.Config.BasePath+SWAGGERUIENDPOINT+"*", handler, pathMiddleware...)

	return nil
}

//RegisterDefaultSwaggerJson registers a default swagger json response
func (p *RESTAPI) RegisterDefaultSwaggerJSON(pathMiddleware []echo.MiddlewareFunc) error {
	p.e.GET(p.Config.BasePath+SWAGGERJSONENDPOINT, func(c echo.Context) error {
		return c.JSON(http.StatusOK, p.Swagger)
	}, pathMiddleware...)
	return nil
}

//Initialize and setup configurations for RESTAPI
func (p *RESTAPI) Initialize(ctxt context.Context) error {
	//register logger
	p.RegisterLog("Default", p.e.Logger)
	//register httpClient
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100
	p.RegisterHTTPClient("Default", &http.Client{
		Transport: t,
		Timeout:   time.Second * 10,
	})
	//register standard controllers
	p.RegisterController("HealthCheck", HealthCheck)
	p.RegisterController("APIDiscovery", APIDiscovery)
	p.RegisterController("DefaultWriteController", DefaultWriteController)
	p.RegisterController("DefaultReadController", DefaultReadController)
	p.RegisterController("DefaultListController", DefaultListController)

	//register standard middleware
	p.RegisterMiddleware("Context", Context)
	p.RegisterMiddleware("Recover", Recover)
	p.RegisterMiddleware("LogLevel", LogLevel)
	p.RegisterMiddleware("ZapLogger", ZapLogger)
	//register standard global initializers
	p.RegisterInitializer("SQLDatabase", SQLDatabase)
	p.RegisterInitializer("DefaultProjection", DefaultProjection)
	p.RegisterInitializer("RegisterEntityRepositories", RegisterEntityRepositories)
	p.RegisterInitializer("DefaultEventStore", DefaultEventStore)
	p.RegisterInitializer("Security", Security)
	//register standard operation initializers
	p.RegisterOperationInitializer(ContextInitializer)
	p.RegisterOperationInitializer(EntityRepositoryInitializer)
	p.RegisterOperationInitializer(UserDefinedInitializer)
	p.RegisterOperationInitializer(AuthorizationInitializer)
	p.RegisterOperationInitializer(StandardInitializer)
	p.RegisterOperationInitializer(RouteInitializer)
	//register standard post path initializers
	p.RegisterPostPathInitializer(CORsInitializer)

	//setup command dispatcher
	if _, err := p.GetCommandDispatcher("Default"); err != nil {
		defaultCommandDispatcher := &model.DefaultCommandDispatcher{}
		//setup default commands
		defaultCommandDispatcher.AddSubscriber(model.Create(context.Background(), nil, "", ""), model.CreateHandler)
		defaultCommandDispatcher.AddSubscriber(model.Update(context.Background(), nil, ""), model.UpdateHandler)
		defaultCommandDispatcher.AddSubscriber(model.Delete(context.Background(), "", "", 0), model.DeleteHandler)
		p.RegisterCommandDispatcher("Default", defaultCommandDispatcher)
	}

	//setup middleware  - https://echo.labstack.com/middleware/

	//setup global pre middleware
	if p.Config != nil && p.Config.Rest != nil {
		var preMiddlewares []echo.MiddlewareFunc
		for _, middlewareName := range p.Config.Rest.PreMiddleware {
			t := reflect.ValueOf(middlewareName)
			m := t.MethodByName(middlewareName)
			if !m.IsValid() {
				p.e.Logger.Fatalf("invalid handler set '%s'", middlewareName)
			}
			preMiddlewares = append(preMiddlewares, m.Interface().(func(handlerFunc echo.HandlerFunc) echo.HandlerFunc))
		}
		//all routes setup after this will use this middleware
		p.e.Pre(preMiddlewares...)
	}

	//setup global middleware
	var middlewares []echo.MiddlewareFunc
	//prepend Context middleware
	//for _, middlewareName := range p.Config.Rest.Middleware {
	//	tmiddleware, err := p.GetMiddleware(middlewareName)
	//	if err != nil {
	//		p.e.Logger.Fatalf("invalid middleware set '%s'. Must be of type rest.Middleware", middlewareName)
	//	}
	//	middlewares = append(middlewares, tmiddleware(p.Application, p.Swagger, nil, nil))
	//}
	//all routes setup after this will use this middleware
	p.e.Use(middlewares...)
	//set log level to debug
	p.EchoInstance().Logger.SetLevel(log.DEBUG)

	//setup routes
	knownActions := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE", "CONNECT"}
	var err error
	globalContext := context.Background()
	//run global initializers
	for _, initializer := range p.GetGlobalInitializers() {
		globalContext, err = initializer(globalContext, p, p.Swagger)
		if err != nil {
			return err
		}
	}
	for path, pathData := range p.Swagger.Paths {
		var methodsFound []string

		//run pre path initializers
		for _, initializer := range p.GetPrePathInitializers() {
			globalContext, err = initializer(globalContext, p, path, p.Swagger, pathData)
			if err != nil {
				return err
			}
		}
		for _, method := range knownActions {
			//get the operation data
			operationData := pathData.GetOperation(strings.ToUpper(method))
			if operationData != nil {
				methodsFound = append(methodsFound, strings.ToUpper(method))
				operationContext := globalContext
				for _, initializer := range p.GetOperationInitializers() {
					operationContext, err = initializer(operationContext, p, path, method, p.Swagger, pathData, operationData)
					if err != nil {
						return err
					}
				}
			}
		}

		//run post path initializers
		globalContext = context.WithValue(globalContext, weoscontext.METHODS_FOUND, methodsFound)
		for _, initializer := range p.GetPostPathInitializers() {
			globalContext, err = initializer(globalContext, p, path, p.Swagger, pathData)
		}
	}
	//output registered endpoints for debugging purposes
	for _, route := range p.EchoInstance().Routes() {
		p.EchoInstance().Logger.Debugf("Registered routes '%s' '%s'", route.Method, route.Path)
	}
	return err
}

//SQLConnectionFromConfig get db connection based on a Config
func (p *RESTAPI) SQLConnectionFromConfig(config *model.DBConfig) (*sql.DB, *gorm.DB, string, error) {
	var connStr string
	var err error

	if config.AwsIam {
		dbName := config.Database
		dbUser := config.User
		dbHost := config.Host
		dbPort := config.Port
		dbEndpoint := fmt.Sprintf("%s:%d", dbHost, dbPort)
		region := config.AwsRegion

		cfg, err := awsconfig.LoadDefaultConfig(context.TODO())
		if err != nil {
			log.Printf("aws configuration error: " + err.Error())
		}

		authenticationToken, err := auth.BuildAuthToken(
			context.TODO(), dbEndpoint, region, dbUser, cfg.Credentials)
		if err != nil {
			log.Printf("failed to create aws authentication token: " + err.Error())
		}

		switch config.Driver {
		case "mysql":
			connStr = fmt.Sprintf("%s:%s@tcp(%s)/%s?tls=true&sql_mode='ERROR_FOR_DIVISION_BY_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION'&allowCleartextPasswords=true&parseTime=true",
				dbUser, authenticationToken, dbEndpoint, dbName,
			)
		case "postgres":
			connStr = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
				dbHost, dbPort, dbUser, authenticationToken, dbName,
			)
		default:
			return nil, nil, "", InvalidAWSDriver
		}
	} else {
		switch config.Driver {
		case "sqlite3":
			//check if file exists and if not create it. We only do this if a memory only db is NOT asked for
			//(Note that if it's a combination we go ahead and create the file) https://www.sqlite.org/inmemorydb.html
			if config.Database != ":memory:" {
				if _, err = os.Stat(config.Database); os.IsNotExist(err) {
					_, err = os.Create(strings.Replace(config.Database, ":memory:", "", -1))
					if err != nil {
						return nil, nil, "", model.NewError(fmt.Sprintf("error creating sqlite database '%s'", config.Database), err)
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
			return nil, nil, connStr, errors.New(fmt.Sprintf("db driver '%s' is not supported ", config.Driver))
		}
	}

	db, err := sql.Open(config.Driver, connStr)
	if err != nil {
		return nil, nil, connStr, errors.New(fmt.Sprintf("error setting up connection to database '%s' with connection '%s'", err, connStr))
	}

	db.SetMaxOpenConns(config.MaxOpen)
	db.SetMaxIdleConns(config.MaxIdle)
	db.SetConnMaxIdleTime(time.Duration(config.MaxIdleTime))

	if p.gormConnection == nil {
		//setup gorm
		switch config.Driver {
		case "postgres":
			p.gormConnection, err = gorm.Open(dialects.NewPostgres(postgres.Config{
				Conn: db,
			}), nil)
			if err != nil {
				return nil, nil, connStr, err
			}
		case "sqlite3":
			p.gormConnection, err = gorm.Open(&dialects.SQLite{
				sqlite.Dialector{
					Conn: db,
				},
			}, nil)
			if err != nil {
				return nil, nil, connStr, err
			}
		case "mysql":
			p.gormConnection, err = gorm.Open(dialects.NewMySQL(mysql.Config{
				Conn: db,
			}), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
			if err != nil {
				return nil, nil, connStr, err
			}
		case "ramsql": //this is for testing
			p.gormConnection = &gorm.DB{}
		case "sqlserver":
			p.gormConnection, err = gorm.Open(sqlserver.New(sqlserver.Config{
				Conn: db,
			}), nil)
			if err != nil {
				return nil, nil, connStr, err
			}
		case "clickhouse":
			p.gormConnection, err = gorm.Open(clickhouse.New(clickhouse.Config{
				Conn: db,
			}), nil)
			if err != nil {
				return nil, nil, connStr, err
			}
		default:
			return nil, nil, connStr, errors.New(fmt.Sprintf("we don't support database driver '%s'", config.Driver))
		}
	} else {
		p.gormConnection.ConnPool = db
	}

	return db, p.gormConnection, connStr, err
}
