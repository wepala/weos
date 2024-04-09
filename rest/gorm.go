package rest

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	_ "github.com/jackc/pgx/v5"
	"github.com/labstack/gommon/log"
	"go.uber.org/fx"
	"golang.org/x/net/context"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var InvalidAWSDriver = errors.New("invalid aws driver specified, must be postgres or mysql")

type GORMParams struct {
	fx.In
	Config *APIConfig
}

type GORMResult struct {
	fx.Out
	GORMDB *gorm.DB
	SQLDB  *sql.DB
}

func NewGORM(p GORMParams) (GORMResult, error) {
	var connStr string
	var err error

	config := p.Config.Database
	if config == nil && len(p.Config.Databases) > 0 {
		config = p.Config.Databases[0]
	}

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
		case "postgres", "pgx":
			connStr = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
				dbHost, dbPort, dbUser, authenticationToken, dbName,
			)
		default:
			return GORMResult{}, InvalidAWSDriver
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
						return GORMResult{}, fmt.Errorf("error creating sqlite database '%s'", config.Database)
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
		case "postgres", "pgx":
			connStr = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
				config.Host, strconv.Itoa(config.Port), config.User, config.Password, config.Database)
		default:
			return GORMResult{}, errors.New(fmt.Sprintf("db driver '%s' is not supported ", config.Driver))
		}
	}

	db, err := sql.Open(config.Driver, connStr)
	if err != nil {
		return GORMResult{}, errors.New(fmt.Sprintf("error setting up connection to database '%s' with connection '%s'", err, connStr))
	}

	db.SetMaxOpenConns(config.MaxOpen)
	db.SetMaxIdleConns(config.MaxIdle)
	db.SetConnMaxIdleTime(time.Duration(config.MaxIdleTime))
	var gormConnection *gorm.DB
	if gormConnection == nil {
		//setup gorm
		switch config.Driver {
		case "postgres", "pgx":
			gormConnection, err = gorm.Open(postgres.New(postgres.Config{
				Conn: db,
			}), nil)
			if err != nil {
				return GORMResult{}, err
			}
		case "sqlite3":
			gormConnection, err = gorm.Open(sqlite.Dialector{
				Conn: db,
			}, nil)
			if err != nil {
				return GORMResult{}, err
			}
		case "mysql":
			gormConnection, err = gorm.Open(mysql.New(mysql.Config{
				Conn: db,
			}), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
			if err != nil {
				return GORMResult{}, err
			}
		case "ramsql": //this is for testing
			gormConnection = &gorm.DB{}
		case "sqlserver":
			gormConnection, err = gorm.Open(sqlserver.New(sqlserver.Config{
				Conn: db,
			}), nil)
			if err != nil {
				return GORMResult{}, err
			}
		default:
			return GORMResult{}, errors.New(fmt.Sprintf("we don't support database driver '%s'", config.Driver))
		}
	} else {
		gormConnection.ConnPool = db
	}

	return GORMResult{
		GORMDB: gormConnection,
		SQLDB:  db,
	}, err
}

type GORMProjectionParams struct {
	fx.In
	GORMDB       *gorm.DB
	EventConfigs []EventHandlerConfig `group:"eventHandlers"`
}

type GORMProjectionResult struct {
	fx.Out
	Dispatcher        EventStore
	DefaultProjection Projection `name:"defaultProjection"`
}

func NewGORMProjection(p GORMProjectionParams) (result GORMProjectionResult, err error) {
	dispatcher := &GORMProjection{
		handlers: make(map[string]map[string][]EventHandler),
		gormDB:   p.GORMDB,
	}
	err = p.GORMDB.AutoMigrate(&Event{}, &BasicResource{})
	if err != nil {
		return result, err
	}
	for _, config := range p.EventConfigs {
		err = dispatcher.AddSubscriber(config)
		if err != nil {
			return result, err
		}
	}
	//add handlers for create, update and delete

	result = GORMProjectionResult{
		Dispatcher:        dispatcher,
		DefaultProjection: dispatcher,
	}
	return result, nil
}

type EventHandlerConfig struct {
	ResourceType string
	Type         string
	Handler      EventHandler
}

// GORMProjection is a projection that uses GORM to persist events
type GORMProjection struct {
	handlers        map[string]map[string][]EventHandler
	handlerPanicked bool
	gormDB          *gorm.DB
}

// Dispatch dispatches the event to the handlers
func (e *GORMProjection) Dispatch(ctx context.Context, logger Log, event *Event, options *EventOptions) []error {
	//mutex helps keep state between routines
	var errors []error
	var wg sync.WaitGroup
	var handlers []EventHandler
	var ok bool
	if globalHandlers := e.handlers[""]; globalHandlers != nil {
		if handlers, ok = globalHandlers[event.Type]; ok {

		}
	}
	if resourceTypeHandlers, ok := e.handlers[event.Meta.ResourceType]; ok {
		if thandlers, ok := resourceTypeHandlers[event.Type]; ok {
			handlers = append(handlers, thandlers...)
		}
	}

	for i := 0; i < len(handlers); i++ {
		handler := handlers[i]
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("handler panicked %s", r)
				}
				wg.Done()
			}()

			err := handler(ctx, logger, event, options)
			if err != nil {
				errors = append(errors, err)
			}

		}()
	}
	wg.Wait()

	return errors
}

// AddSubscriber adds a subscriber to the event dispatcher
func (e *GORMProjection) AddSubscriber(handler EventHandlerConfig) error {
	if handler.Handler == nil {
		return fmt.Errorf("event handler cannot be nil")
	}
	if e.handlers == nil {
		e.handlers = make(map[string]map[string][]EventHandler)
	}
	if _, ok := e.handlers[handler.ResourceType]; !ok {
		e.handlers[handler.ResourceType] = make(map[string][]EventHandler)
	}
	if _, ok := e.handlers[handler.ResourceType][handler.Type]; !ok {
		e.handlers[handler.ResourceType][handler.Type] = make([]EventHandler, 0)
	}
	e.handlers[handler.ResourceType][handler.Type] = append(e.handlers[handler.ResourceType][handler.Type], handler.Handler)
	return nil
}

func (e *GORMProjection) GetSubscribers(resourceType string) map[string][]EventHandler {
	if handlers, ok := e.handlers[resourceType]; ok {
		return handlers
	}
	return nil
}

func (e *GORMProjection) GetByURI(ctxt context.Context, logger Log, uri string) (Resource, error) {
	resource := new(BasicResource)
	result := e.gormDB.Where("id = ?", uri).First(resource)
	if result.Error != nil {
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, result.Error
		} else {
			return nil, nil
		}
	}
	return resource, nil
}

func (e *GORMProjection) GetByKey(ctxt context.Context, identifiers map[string]interface{}) (Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (e *GORMProjection) GetList(ctx context.Context, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]Resource, int64, error) {
	//TODO implement me
	panic("implement me")
}

func (e *GORMProjection) GetByProperties(ctxt context.Context, identifiers map[string]interface{}) ([]Entity, error) {
	//TODO implement me
	panic("implement me")
}

// Persist persists the events to the database
func (e *GORMProjection) Persist(ctxt context.Context, logger Log, resources []Resource) (errs []error) {
	var events []*Event
	for _, resource := range resources {
		if event, ok := resource.(*Event); ok {
			events = append(events, event)
		} else {
			errs = append(errs, errors.New("resource is not an event"))
		}
	}
	result := e.gormDB.Save(events)
	if result.Error != nil {
		errs = append(errs, result.Error)
	}
	for _, event := range events {
		e.Dispatch(ctxt, logger, event, &EventOptions{
			GORMDB: e.gormDB,
		})
	}
	return errs
}

func (e *GORMProjection) Remove(ctxt context.Context, logger Log, resources []Resource) []error {
	//TODO implement me
	panic("implement me")
}

func (e *GORMProjection) GetEventHandlers() []EventHandlerConfig {
	return []EventHandlerConfig{
		{
			ResourceType: "",
			Type:         "create",
			Handler:      e.ResourceUpdateHandler,
		},
		{
			ResourceType: "",
			Type:         "update",
			Handler:      e.ResourceUpdateHandler,
		},
		{
			ResourceType: "",
			Type:         "delete",
			Handler:      e.ResourceDeleteHandler,
		},
	}
}

// ResourceUpdateHandler handles Create Update operations
func (e *GORMProjection) ResourceUpdateHandler(ctx context.Context, logger Log, event *Event, options *EventOptions) (err error) {
	basicResource := new(BasicResource)
	err = json.Unmarshal(event.Payload, &basicResource)
	if err != nil {
		return err
	}
	result := options.GORMDB.Save(basicResource)
	if result.Error != nil {
		return result.Error
	}
	return err
}

// ResourceDeleteHandler handles Delete operations
func (e *GORMProjection) ResourceDeleteHandler(ctx context.Context, logger Log, event *Event, options *EventOptions) (err error) {
	basicResource := new(BasicResource)
	err = json.Unmarshal(event.Payload, &basicResource)
	if err != nil {
		return err
	}
	result := options.GORMDB.Delete(basicResource)
	if result.Error != nil {
		return result.Error
	}
	return err
}

// List Query Stuff

type QueryFilterModifier func(options map[string]FilterProperty) func(db *gorm.DB) *gorm.DB

var FilterQuery = func(options map[string]FilterProperty) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if options != nil {
			for _, filter := range options {
				operator := "="
				switch filter.Operator {
				case "gt":
					operator = ">"
				case "lt":
					operator = "<"
				case "ne":
					operator = "!="
				case "like":
					if db.Dialector.Name() == "postgres" {
						operator = "ILIKE"
					} else {
						operator = " LIKE"
					}
				case "in":
					operator = "IN"

				}

				if len(filter.Values) == 0 {
					if filter.Operator == "like" {
						db.Where(SnakeCase(filter.Field)+" "+operator+" ?", "%"+filter.Value.(string)+"%")
					} else {
						db.Where(SnakeCase(filter.Field)+" "+operator+" ?", filter.Value)
					}

				} else {
					db.Where(SnakeCase(filter.Field)+" "+operator+" ?", filter.Values)
				}

			}
		}
		return db
	}
}
