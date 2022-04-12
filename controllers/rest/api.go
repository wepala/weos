package rest

import (
	"context"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/wepala/gormstore/v2"
	"net/http"
	"os"
	"reflect"
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

//RESTAPI is used to manage the API
type RESTAPI struct {
	Application                    model.Service
	Log                            model.Log
	DB                             *sql.DB
	Client                         *http.Client
	projection                     *projections.GORMDB
	Config                         *APIConfig
	e                              *echo.Echo
	PathConfigs                    map[string]*PathConfig
	Schemas                        map[string]ds.Builder
	Swagger                        *openapi3.Swagger
	sessionStore                   sessions.Store
	middlewares                    map[string]Middleware
	controllers                    map[string]Controller
	eventStores                    map[string]model.EventRepository
	commandDispatchers             map[string]model.CommandDispatcher
	projections                    map[string]projections.Projection
	globalInitializers             []GlobalInitializer
	operationInitializers          []OperationInitializer
	registeredInitializers         map[reflect.Value]int
	prePathInitializers            []PathInitializer
	registeredPrePathInitializers  map[reflect.Value]int
	postPathInitializers           []PathInitializer
	registeredPostPathInitializers map[reflect.Value]int
	entityFactories                map[string]model.EntityFactory
	dbConnections                  map[string]*sql.DB
	gormConnections                map[string]*gorm.DB
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

//RegisterGlobalInitializer add global initializer if it's not already there
func (p *RESTAPI) RegisterGlobalInitializer(initializer GlobalInitializer) {
	if p.registeredInitializers == nil {
		p.registeredInitializers = make(map[reflect.Value]int)
	}
	//only add initializer if it doesn't already exist
	tpoint := reflect.ValueOf(initializer)
	if _, ok := p.registeredInitializers[tpoint]; !ok {
		p.globalInitializers = append(p.globalInitializers, initializer)
		p.registeredInitializers[tpoint] = len(p.globalInitializers)
	}

}

//RegisterOperationInitializer add operation initializer if it's not already there
func (p *RESTAPI) RegisterOperationInitializer(initializer OperationInitializer) {
	if p.registeredInitializers == nil {
		p.registeredInitializers = make(map[reflect.Value]int)
	}
	//only add initializer if it doesn't already exist
	tpoint := reflect.ValueOf(initializer)
	if _, ok := p.registeredInitializers[tpoint]; !ok {
		p.operationInitializers = append(p.operationInitializers, initializer)
		p.registeredInitializers[tpoint] = len(p.operationInitializers)
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
func (p *RESTAPI) RegisterProjection(name string, projection projections.Projection) {
	if p.projections == nil {
		p.projections = make(map[string]projections.Projection)
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
	if p.gormConnections == nil {
		p.gormConnections = make(map[string]*gorm.DB)
	}
	p.gormConnections[name] = connection
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
		return tmiddleware.Interface().(func(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc), nil
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
		return tcontroller.Interface().(func(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc), nil
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
func (p *RESTAPI) GetProjection(name string) (projections.Projection, error) {
	if tdispatcher, ok := p.projections[name]; ok {
		return tdispatcher, nil
	}
	return nil, fmt.Errorf("projection '%s' not found", name)
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

//GetDBConnection get db connection by name
func (p *RESTAPI) GetDBConnection(name string) (*sql.DB, error) {
	if tconnection, ok := p.dbConnections[name]; ok {
		return tconnection, nil
	}
	return nil, fmt.Errorf("database connection '%s' not found", name)
}

//GetGormDBConnection get gorm connection by name
func (p *RESTAPI) GetGormDBConnection(name string) (*gorm.DB, error) {
	if tconnection, ok := p.gormConnections[name]; ok {
		return tconnection, nil
	}
	return nil, fmt.Errorf("gorm database connection '%s' not found", name)
}

//GetSessionStore get gorm session store
func (p *RESTAPI) GetSessionStore() sessions.Store {
	return p.sessionStore
}

//GetSession get sesssion from database
func (p *RESTAPI) GetSession(sessionID, sessionName string) (*sessions.Session, error) {
	gormDB := p.gormConnections["Default"]
	if gormDB == nil {
		p.e.Logger.Errorf("unexpected error: default gormDB is not initialized")
		return nil, fmt.Errorf("unexpected error: default gormDB is not initialized")
	}
	var sessionInfo map[string]interface{}
	var data map[interface{}]interface{}
	result := gormDB.Table("sessions").Find(&sessionInfo, "id = ?", sessionID)
	if result.Error != nil {
		p.e.Logger.Error(result.Error)
		return nil, result.Error
	}
	if sessionInfo == nil {
		session, _ := p.sessionStore.Get(&http.Request{}, sessionName)
		return session, nil
	}
	if sessionInfo["data"] != nil {
		//this assumes that sessionstore on the api is a gormstore by default
		err := securecookie.DecodeMulti(sessionName, sessionInfo["data"].(string), &data, p.sessionStore.(*gormstore.Store).Codecs...)
		if err != nil {
			return nil, err
		}
	}
	session, _ := p.sessionStore.Get(&http.Request{}, sessionName)
	session.ID = sessionID
	session.Values = data
	session.IsNew = false

	return session, nil
}

type gormSession struct {
	ID        string `sql:"unique_index"`
	Data      string `sql:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time `sql:"index"`
}

//SaveSession save sesssion to database and set cookie header
func (p *RESTAPI) SaveSession(w http.ResponseWriter, session *sessions.Session) error {
	if session == nil {
		fmt.Errorf("unexpected error session is nil")
	}
	s := &gormSession{}
	// delete if max age is < 0
	if session.Options.MaxAge < 0 {
		if session != nil {
			s.ID = session.ID
			if err := p.gormConnections["Default"].Table("sessions").Delete(s).Error; err != nil {
				return err
			}
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
		return nil
	}
	data, err := securecookie.EncodeMulti(session.Name(), session.Values, p.sessionStore.(*gormstore.Store).Codecs...)
	if err != nil {
		return err
	}
	now := time.Now()
	expire := now.Add(time.Second * time.Duration(session.Options.MaxAge))
	if session.ID == "" {
		// generate random session ID key suitable for storage in the db
		session.ID = strings.TrimRight(
			base32.StdEncoding.EncodeToString(
				securecookie.GenerateRandomKey(32)), "=")
		s := &gormSession{
			ID:        session.ID,
			Data:      data,
			CreatedAt: now,
			UpdatedAt: now,
			ExpiresAt: expire,
		}
		if err = p.gormConnections["Default"].Table("sessions").Create(s).Error; err != nil {
			return err
		}
	} else {
		s.ID = session.ID
		s.Data = data
		s.UpdatedAt = now
		s.ExpiresAt = expire
		if err = p.gormConnections["Default"].Table("sessions").Save(s).Error; err != nil {
			return err
		}
	}
	http.SetCookie(w, sessions.NewCookie(session.Name(), session.ID, session.Options))
	return nil
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
	sh := http.StripPrefix(SWAGGERUIENDPOINT, static)
	handler := echo.WrapHandler(sh)
	p.e.GET(SWAGGERUIENDPOINT+"*", handler, pathMiddleware...)

	return nil
}

//RegisterDefaultSwaggerJson registers a default swagger json response
func (p *RESTAPI) RegisterDefaultSwaggerJSON(pathMiddleware []echo.MiddlewareFunc) error {
	p.e.GET(SWAGGERJSONENDPOINT, func(c echo.Context) error {
		return c.JSON(http.StatusOK, p.Swagger)
	}, pathMiddleware...)
	return nil
}

//Initialize and setup configurations for RESTAPI
func (p *RESTAPI) Initialize(ctxt context.Context) error {
	//register standard controllers
	p.RegisterController("CreateController", CreateController)
	p.RegisterController("UpdateController", UpdateController)
	p.RegisterController("ListController", ListController)
	p.RegisterController("ViewController", ViewController)
	p.RegisterController("DeleteController", DeleteController)
	p.RegisterController("HealthCheck", HealthCheck)
	p.RegisterController("CreateBatchController", CreateBatchController)
	p.RegisterController("APIDiscovery", APIDiscovery)
	p.RegisterController("DefaultResponseController", DefaultResponseController)

	//register standard middleware
	p.RegisterMiddleware("Context", Context)
	p.RegisterMiddleware("OpenIDMiddleware", OpenIDMiddleware)
	p.RegisterMiddleware("CreateMiddleware", CreateMiddleware)
	p.RegisterMiddleware("CreateBatchMiddleware", CreateBatchMiddleware)
	p.RegisterMiddleware("UpdateMiddleware", UpdateMiddleware)
	p.RegisterMiddleware("ListMiddleware", ListMiddleware)
	p.RegisterMiddleware("ViewMiddleware", ViewMiddleware)
	p.RegisterMiddleware("DeleteMiddleware", DeleteMiddleware)
	p.RegisterMiddleware("Recover", Recover)
	p.RegisterMiddleware("ContentTypeResponseMiddleware", ContentTypeResponseMiddleware)
	p.RegisterMiddleware("DefaultResponseMiddleware", DefaultResponseMiddleware)
	p.RegisterMiddleware("LogLevel", LogLevel)
	p.RegisterMiddleware("ZapLogger", ZapLogger)
	//register standard global initializers
	p.RegisterGlobalInitializer(SQLDatabase)
	p.RegisterGlobalInitializer(DefaultProjection)
	p.RegisterGlobalInitializer(DefaultEventStore)
	p.RegisterGlobalInitializer(Security)
	//register standard operation initializers
	p.RegisterOperationInitializer(ContextInitializer)
	p.RegisterOperationInitializer(ContentTypeResponseInitializer)
	p.RegisterOperationInitializer(EntityFactoryInitializer)
	p.RegisterOperationInitializer(UserDefinedInitializer)
	p.RegisterOperationInitializer(StandardInitializer)
	p.RegisterOperationInitializer(RouteInitializer)
	//register standard post path initializers
	p.RegisterPostPathInitializer(CORsInitializer)

	//setup command dispatcher
	if _, err := p.GetCommandDispatcher("Default"); err != nil {
		defaultCommandDispatcher := &model.DefaultCommandDispatcher{}
		//setup default commands
		defaultCommandDispatcher.AddSubscriber(model.Create(context.Background(), nil, "", ""), model.CreateHandler)
		defaultCommandDispatcher.AddSubscriber(model.CreateBatch(context.Background(), nil, ""), model.CreateBatchHandler)
		defaultCommandDispatcher.AddSubscriber(model.Update(context.Background(), nil, ""), model.UpdateHandler)
		defaultCommandDispatcher.AddSubscriber(model.Delete(context.Background(), "", ""), model.DeleteHandler)
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
		//output registered endpoints for debugging purposes
		for _, route := range p.EchoInstance().Routes() {
			p.EchoInstance().Logger.Debugf("Registered routes '%s' '%s'", route.Method, route.Path)
		}
	}
	return err
}

//SQLConnectionFromConfig get db connection based on a Config
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
