package model

//go:generate moq -out mocks_test.go -pkg weos_test . EventRepository Projection Log Dispatcher Application

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type ApplicationConfig struct {
	ModuleID      string     `json:"moduleId"`
	Title         string     `json:"title"`
	AccountID     string     `json:"accountId"`
	ApplicationID string     `json:"applicationId"`
	AccountName   string     `json:"accountName"`
	Database      *DBConfig  `json:"database"`
	Log           *LogConfig `json:"log"`
	BaseURL       string     `json:"baseURL"`
	LoginURL      string     `json:"loginURL"`
	GraphQLURL    string     `json:"graphQLURL"`
	SessionKey    string     `json:"sessionKey"`
	Secret        string     `json:"secret"`
	AccountURL    string     `json:"accountURL"`
}

type DBConfig struct {
	Host     string `json:"host"`
	User     string `json:"username"`
	Password string `json:"password"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Driver   string `json:"driver"`
	MaxOpen  int    `json:"max-open"`
	MaxIdle  int    `json:"max-idle"`
}

type LogConfig struct {
	Level        string `json:"level"`
	ReportCaller bool   `json:"report-caller"`
	Formatter    string `json:"formatter"`
}

type Application interface {
	ID() string
	Title() string
	DBConnection() *sql.DB
	DB() *gorm.DB
	Logger() Log
	AddProjection(projection Projection) error
	Projections() []Projection
	Migrate(ctx context.Context) error
	Config() *ApplicationConfig
	EventRepository() EventRepository
	HTTPClient() *http.Client
	Dispatcher() Dispatcher
}

//Module is the core of the WeOS framework. It has a config, command handler and basic metadata as a default.
//This is a basic implementation and can be overwritten to include a db connection, httpCLient etc.
type BaseApplication struct {
	id              string
	title           string
	logger          Log
	dbConnection    *sql.DB
	db              *gorm.DB
	config          *ApplicationConfig
	projections     []Projection
	eventRepository EventRepository
	httpClient      *http.Client
	dispatcher      Dispatcher
}

func (w *BaseApplication) Logger() Log {
	return w.logger
}

func (w *BaseApplication) Config() *ApplicationConfig {
	return w.config
}

func (w *BaseApplication) ID() string {
	return w.id
}

func (w *BaseApplication) Title() string {
	return w.title
}

func (w *BaseApplication) DBConnection() *sql.DB {
	return w.dbConnection
}

func (w *BaseApplication) AddProjection(projection Projection) error {
	w.projections = append(w.projections, projection)
	if w.eventRepository != nil {
		w.eventRepository.AddSubscriber(projection.GetEventHandler())
	}
	return nil
}

func (w *BaseApplication) Projections() []Projection {
	return w.projections
}

func (w *BaseApplication) DB() *gorm.DB {
	return w.db
}

func (w *BaseApplication) Migrate(ctx context.Context) error {
	w.logger.Infof("preparing to migrate %d projections", len(w.projections))
	for _, projection := range w.projections {
		err := projection.Migrate(ctx)
		if err != nil {
			return err
		}
	}

	err := w.EventRepository().Migrate(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (w *BaseApplication) EventRepository() EventRepository {
	return w.eventRepository
}

func (w *BaseApplication) HTTPClient() *http.Client {
	return w.httpClient
}

func (w *BaseApplication) Dispatcher() Dispatcher {
	return w.dispatcher
}

var NewApplicationFromConfig = func(config *ApplicationConfig, logger Log, db *sql.DB, client *http.Client, eventRepository EventRepository) (*BaseApplication, error) {

	var err error

	if logger == nil {
		//	if config.Log.Level != "" {
		//		switch config.Log.Level {
		//		case "debug":
		//			log.SetLevel(log.DebugLevel)
		//			break
		//		case "fatal":
		//			log.SetLevel(log.FatalLevel)
		//			break
		//		case "error":
		//			log.SetLevel(log.ErrorLevel)
		//			break
		//		case "warn":
		//			log.SetLevel(log.WarnLevel)
		//			break
		//		case "info":
		//			log.SetLevel(log.InfoLevel)
		//			break
		//		case "trace":
		//			log.SetLevel(log.TraceLevel)
		//			break
		//		}
		//	}
		//
		//	if config.Log.Formatter == "json" {
		//		log.SetFormatter(&log.JSONFormatter{})
		//	}
		//
		//	if config.Log.Formatter == "text" {
		//		log.SetFormatter(&log.TextFormatter{})
		//	}
		//
		//	log.SetReportCaller(config.Log.ReportCaller)
		//
		logger = log.New()
	}

	if db == nil && config.Database != nil {
		var connStr string

		switch config.Database.Driver {
		case "sqlite3":
			//check if file exists and if not create it. We only do this if a memory only db is NOT asked for
			//(Note that if it's a combination we go ahead and create the file) https://www.sqlite.org/inmemorydb.html
			if config.Database.Database != ":memory:" {
				if _, err = os.Stat(config.Database.Database); os.IsNotExist(err) {
					_, err = os.Create(strings.Replace(config.Database.Database, ":memory:", "", -1))
					if err != nil {
						return nil, NewError(fmt.Sprintf("error creating sqlite database '%s'", config.Database.Database), err)
					}
				}
			}

			connStr = fmt.Sprintf("%s",
				config.Database.Database)

			//update connection string to include authentication IF a username is set
			if config.Database.User != "" {
				authenticationString := fmt.Sprintf("?_auth&_auth_user=%s&_auth_pass=%s&_auth_crypt=sha512&_foreign_keys=on",
					config.Database.User, config.Database.Password)
				connStr = connStr + authenticationString
			} else {
				connStr = connStr + "?_foreign_keys=on"
			}
			log.Debugf("sqlite connection string '%s'", connStr)
		case "sqlserver":
			connStr = fmt.Sprintf("sqlserver://%s:%s@%s:%s/%s",
				config.Database.User, config.Database.Password, config.Database.Host, strconv.Itoa(config.Database.Port), config.Database.Database)
		case "ramsql":
			connStr = "Testing"
		case "mysql":
			connStr = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?sql_mode='ERROR_FOR_DIVISION_BY_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION'&parseTime=true",
				config.Database.User, config.Database.Password, config.Database.Host, strconv.Itoa(config.Database.Port), config.Database.Database)
		case "clickhouse":
			connStr = fmt.Sprintf("tcp://%s:%s?username=%s&password=%s&database=%s",
				config.Database.Host, strconv.Itoa(config.Database.Port), config.Database.User, config.Database.Password, config.Database.Database)
		case "postgres":
			connStr = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
				config.Database.Host, strconv.Itoa(config.Database.Port), config.Database.User, config.Database.Password, config.Database.Database)
		default:
			return nil, errors.New(fmt.Sprintf("db driver '%s' is not supported ", config.Database.Driver))
		}

		db, err = sql.Open(config.Database.Driver, connStr)
		if err != nil {
			logger.Errorf("connection string '%s'", connStr)
			return nil, NewError(fmt.Sprintf("error setting up connection to database '%s' with connection '%s'", err, connStr), err)
		}

		db.SetMaxOpenConns(config.Database.MaxOpen)
		db.SetMaxIdleConns(config.Database.MaxIdle)

	}

	//setup gorm connection
	var gormDB *gorm.DB
	switch config.Database.Driver {
	case "postgres":
		gormDB, err = gorm.Open(postgres.New(postgres.Config{
			Conn: db,
		}), nil)
		if err != nil {
			return nil, err
		}
	case "sqlite3":
		gormDB, err = gorm.Open(&sqlite.Dialector{
			Conn: db,
		}, nil)
		if err != nil {
			return nil, err
		}
	case "mysql":
		gormDB, err = gorm.Open(mysql.New(mysql.Config{
			Conn: db,
		}), nil)
		if err != nil {
			return nil, err
		}
	case "ramsql": //this is for testing
		gormDB = &gorm.DB{}
	case "sqlserver":
		gormDB, err = gorm.Open(sqlserver.New(sqlserver.Config{
			Conn: db,
		}), nil)
		if err != nil {
			return nil, err
		}
	case "clickhouse":
		gormDB, err = gorm.Open(clickhouse.New(clickhouse.Config{
			Conn: db,
		}), nil)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New(fmt.Sprintf("we don't support database driver '%s'", config.Database.Driver))
	}

	if client == nil {
		client = &http.Client{
			Timeout: time.Second * 10,
		}
	}

	if eventRepository == nil {
		eventRepository, err = NewBasicEventRepository(gormDB, logger, false, config.AccountID, config.ApplicationID)
		if err != nil {
			return nil, err
		}
	}

	return &BaseApplication{
		id:              config.ModuleID,
		title:           config.Title,
		logger:          logger,
		dbConnection:    db,
		db:              gormDB,
		config:          config,
		httpClient:      client,
		eventRepository: eventRepository,
		dispatcher:      &DefaultCommandDispatcher{},
	}, nil
}
