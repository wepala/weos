package rest

import (
	"database/sql"
	"errors"
	"fmt"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
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
		case "postgres":
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
		case "postgres":
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
		case "postgres":
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

type GORMResourceRepositoryParams struct {
	fx.In
	GormDB *gorm.DB
}

type GORMResourceRepositoryResult struct {
	fx.Out
	Repository *GORMResourceRepository
}

func NewGORMResourceRepository(p GORMResourceRepositoryParams) GORMResourceRepositoryResult {
	return GORMResourceRepositoryResult{
		Repository: &GORMResourceRepository{
			db: p.GormDB,
		},
	}
}

type GORMResourceRepository struct {
	db *gorm.DB
}

func (w GORMResourceRepository) GetByURI(ctxt context.Context, logger Log, uri string) (resource *BasicResource, err error) {
	w.db.First(&resource, "id = ?", uri)
	err = w.db.Error
	return resource, err
}

func (w GORMResourceRepository) Save(ctxt context.Context, logger Log, resource *BasicResource) error {
	result := w.db.Save(resource)
	return result.Error
}

func (w GORMResourceRepository) Delete(ctxt context.Context, logger Log, resource *BasicResource) error {
	result := w.db.Delete(resource)
	return result.Error
}

type GORMEventStoreParams struct {
	fx.In
	GORMDB       *gorm.DB
	EventConfigs []EventHandlerConfig `group:"eventHandlers"`
}

type GORMEventStoreResult struct {
	fx.Out
	Dispatcher EventDispatcher
}

func NewGORMEventStore(p GORMEventStoreParams) GORMEventStoreResult {
	dispatcher := &GORMEventStore{
		handlers: make(map[string]map[string][]EventHandler),
	}
	for _, config := range p.EventConfigs {
		dispatcher.AddSubscriber(config)
	}
	return GORMEventStoreResult{}
}

type EventHandlerConfig struct {
	ResourceType string
	Type         string
	Handler      EventHandler
}

type GORMEventStore struct {
	handlers        map[string]map[string][]EventHandler
	handlerPanicked bool
}

func (e *GORMEventStore) Dispatch(ctx context.Context, event Event, logger Log) []error {
	//mutex helps keep state between routines
	var errors []error
	var wg sync.WaitGroup
	if resourceTypeHandlers, ok := e.handlers[event.Meta.ResourceType]; ok {

		if handlers, ok := resourceTypeHandlers[event.Type]; ok {
			//check to see if there were handlers registered for the event type that is not specific to a resource type
			if event.Meta.ResourceType != "" {
				if eventTypeHandlers, ok := e.handlers[""]; ok {
					if ehandlers, ok := eventTypeHandlers[event.Type]; ok {
						handlers = append(handlers, ehandlers...)
					}
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

					err := handler(ctx, logger, event)
					if err != nil {
						errors = append(errors, err)
					}

				}()
			}
			wg.Wait()
		}

	}

	return errors
}

func (e *GORMEventStore) AddSubscriber(handler EventHandlerConfig) error {
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

func (e *GORMEventStore) GetSubscribers(resourceType string) map[string][]EventHandler {
	if handlers, ok := e.handlers[resourceType]; ok {
		return handlers
	}
	return nil
}

func (e *GORMEventStore) GetByURI(ctxt context.Context, logger Log, uri string) (Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (e *GORMEventStore) GetByKey(ctxt context.Context, identifiers map[string]interface{}) (Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (e *GORMEventStore) GetList(ctx context.Context, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]Resource, int64, error) {
	//TODO implement me
	panic("implement me")
}

func (e *GORMEventStore) GetByProperties(ctxt context.Context, identifiers map[string]interface{}) ([]Entity, error) {
	//TODO implement me
	panic("implement me")
}
