package model

//go:generate moq -out mocks_test.go -pkg model_test . EventRepository Projection Log CommandDispatcher Service EntityFactory

import (
	"database/sql"
	"net/http"
	"time"

	ds "github.com/ompluscator/dynamic-struct"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/net/context"
	"gorm.io/gorm"
)

type ServiceConfig struct {
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

type Service interface {
	ID() string
	Title() string
	DBConnection() *sql.DB
	DB() *gorm.DB
	Logger() Log
	AddProjection(projection Projection) error
	Projections() []Projection
	Migrate(ctx context.Context, builders map[string]ds.Builder) error
	Config() *ServiceConfig
	EventRepository() EventRepository
	HTTPClient() *http.Client
	Dispatcher() CommandDispatcher
}

//Deprecated: 01/30/2022 Removing this in favor of instantiating things during initialize and then passing by injecting
//them into the routes.
//Module is the core of the WeOS framework. It has a config, command handler and basic metadata as a default.
//This is a basic implementation and can be overwritten to include a db connection, httpCLient etc.
type BaseService struct {
	id              string
	title           string
	logger          Log
	dbConnection    *sql.DB
	db              *gorm.DB
	config          *ServiceConfig
	projections     []Projection
	eventRepository EventRepository
	httpClient      *http.Client
	dispatcher      CommandDispatcher
}

func (w *BaseService) Logger() Log {
	return w.logger
}

func (w *BaseService) Config() *ServiceConfig {
	return w.config
}

func (w *BaseService) ID() string {
	return w.id
}

func (w *BaseService) Title() string {
	return w.title
}

func (w *BaseService) DBConnection() *sql.DB {
	return w.dbConnection
}

func (w *BaseService) AddProjection(projection Projection) error {
	w.projections = append(w.projections, projection)
	if w.eventRepository != nil {
		w.eventRepository.AddSubscriber(projection.GetEventHandler())
	}
	return nil
}

func (w *BaseService) Projections() []Projection {
	return w.projections
}

func (w *BaseService) DB() *gorm.DB {
	return w.db
}

func (w *BaseService) Migrate(ctx context.Context, builders map[string]ds.Builder) error {
	w.logger.Infof("preparing to migrate %d projections", len(w.projections))
	for _, projection := range w.projections {
		err := projection.Migrate(ctx, nil, nil)
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

func (w *BaseService) EventRepository() EventRepository {
	return w.eventRepository
}

func (w *BaseService) HTTPClient() *http.Client {
	return w.httpClient
}

func (w *BaseService) Dispatcher() CommandDispatcher {
	return w.dispatcher
}

//Deprecated: Dependency injection is used so that there no longer is a need to create a struct that holds the api information
var NewApplicationFromConfig = func(config *ServiceConfig, logger Log, db *sql.DB, client *http.Client, eventRepository EventRepository) (*BaseService, error) {

	if client == nil {
		client = &http.Client{
			Timeout: time.Second * 10,
		}
	}

	//if eventRepository == nil {
	//	eventRepository, err = NewBasicEventRepository(gormDB, logger, false, config.AccountID, config.ApplicationID)
	//	if err != nil {
	//		return nil, err
	//	}
	//}

	return &BaseService{
		id:              config.ModuleID,
		title:           config.Title,
		logger:          logger,
		dbConnection:    db,
		config:          config,
		httpClient:      client,
		eventRepository: eventRepository,
		dispatcher:      &DefaultCommandDispatcher{},
	}, nil
}
