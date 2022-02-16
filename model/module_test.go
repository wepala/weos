package model_test

import (
	"database/sql"
	"os"
	"testing"

	dynamicstruct "github.com/ompluscator/dynamic-struct"
	_ "github.com/proullon/ramsql/driver"
	"github.com/wepala/weos/controllers/rest"
	weos "github.com/wepala/weos/model"
	"golang.org/x/net/context"
)

func TestNewApplicationFromConfig(t *testing.T) {
	t.SkipNow()
	config := &weos.ServiceConfig{
		ModuleID:  "1iPwGftUqaP4rkWdvFp6BBW2tOf",
		Title:     "Test Module",
		AccountID: "1iPwIGTgWVGyl4XfgrhCqYiiQ7d",
		Database: &weos.DBConfig{
			Driver:   "ramsql",
			Host:     "localhost",
			User:     "root",
			Password: "password",
			Port:     5432,
			Database: "test",
		},
		Log: &weos.LogConfig{
			Level:        "debug",
			ReportCaller: false,
			Formatter:    "text",
		},
	}

	t.Run("basic module from config", func(t *testing.T) {
		app, err := weos.NewApplicationFromConfig(config, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("error encountered setting up app '%s'", err)
		}
		if app.ID() != config.ModuleID {
			t.Errorf("expected the module id to be '%s', got '%s'", config.ModuleID, app.ID())
		}

		if app.DBConnection() == nil {
			t.Error("expected the db connection to be setup")
		}

		if app.Logger() == nil {
			t.Error("expected the logger to be setup")
		}

		if app.HTTPClient() == nil {
			t.Error("expected the default http client to be setup")
		}

		if app.Dispatcher() == nil {
			t.Error("expected the default command dispatcher to be setup")
		}

		if app.EventRepository() == nil {
			t.Errorf("expected a default event repository to be setup")
		}
	})

	t.Run("override logger", func(t *testing.T) {
		logger := &LogMock{
			DebugFunc: func(args ...interface{}) {

			},
		}
		app, err := weos.NewApplicationFromConfig(config, logger, nil, nil, &EventRepositoryMock{})
		if err != nil {
			t.Fatalf("error encountered setting up app")
		}
		app.Logger().Debug("some debug")
		if len(logger.DebugCalls()) == 0 {
			t.Errorf("expected the debug function on the logger to be called at least %d time, called %d times", 1, len(logger.DebugCalls()))
		}
	})

	t.Run("override db connection", func(t *testing.T) {
		db, err := sql.Open("ramsql", "TestLoadUserAddresses")
		if err != nil {
			t.Fatalf("sql.Open : Error : %s\n", err)
		}
		defer db.Close()

		app, err := weos.NewApplicationFromConfig(config, nil, db, nil, &EventRepositoryMock{})
		if err != nil {
			t.Fatalf("error encountered setting up app")
		}
		if app.DBConnection().Ping() != nil {
			t.Errorf("didn't expect errors pinging the database")
		}
	})
}

func TestNewApplicationFromConfig_SQLite(t *testing.T) {
	t.Run("test setting up basic sqlite connection", func(t *testing.T) {
		sqliteConfig := &weos.ServiceConfig{
			ModuleID:  "1iPwGftUqaP4rkWdvFp6BBW2tOf",
			Title:     "Test Module",
			AccountID: "1iPwIGTgWVGyl4XfgrhCqYiiQ7d",
			Database: &weos.DBConfig{
				Driver:   "sqlite3",
				Database: "test.db",
			},
			Log: &weos.LogConfig{
				Level:        "debug",
				ReportCaller: false,
				Formatter:    "text",
			},
		}

		api := &rest.RESTAPI{}
		db, _, err := api.SQLConnectionFromConfig(sqliteConfig.Database)
		if err != nil {
			t.Fatalf("unexpected error getting connection '%s'", err)
		}

		if db.Ping() != nil {
			t.Errorf("didn't expect errors pinging the database")
		}

		//cleanup file after test
		err = os.Remove(sqliteConfig.Database.Database)
		if err != nil {
			t.Fatalf("error cleaning up '%s'", err)
		}
	})

	t.Run("test setting up sqlite connection in memory named database", func(t *testing.T) {
		sqliteConfig := &weos.ServiceConfig{
			ModuleID:  "1iPwGftUqaP4rkWdvFp6BBW2tOf",
			Title:     "Test Module",
			AccountID: "1iPwIGTgWVGyl4XfgrhCqYiiQ7d",
			Database: &weos.DBConfig{
				Driver:   "sqlite3",
				Database: ":memory:",
			},
			Log: &weos.LogConfig{
				Level:        "debug",
				ReportCaller: false,
				Formatter:    "text",
			},
		}

		api := &rest.RESTAPI{}
		db, _, err := api.SQLConnectionFromConfig(sqliteConfig.Database)
		if err != nil {
			t.Fatalf("unexpected error getting connection '%s'", err)
		}

		if db.Ping() != nil {
			t.Errorf("didn't expect errors pinging the database")
		}

		if _, err = os.Stat(sqliteConfig.Database.Database); err == nil {
			t.Errorf("database was not created in memory")
			//cleanup file
			err = os.Remove(sqliteConfig.Database.Database)
			if err != nil {
				t.Fatalf("error cleaning up '%s'", err)
			}
		}
	})

	t.Run("test setting up sqlite connection with authentication", func(t *testing.T) {
		sqliteConfig := &weos.ServiceConfig{
			ModuleID:  "1iPwGftUqaP4rkWdvFp6BBW2tOf",
			Title:     "Test Module",
			AccountID: "1iPwIGTgWVGyl4XfgrhCqYiiQ7d",
			Database: &weos.DBConfig{
				Driver:   "sqlite3",
				Database: ":memory:",
				User:     "test",
				Password: "pass",
			},
			Log: &weos.LogConfig{
				Level:        "debug",
				ReportCaller: false,
				Formatter:    "text",
			},
		}

		api := &rest.RESTAPI{}
		db, _, err := api.SQLConnectionFromConfig(sqliteConfig.Database)
		if err != nil {
			t.Fatalf("unexpected error getting connection '%s'", err)
		}

		if db.Ping() != nil {
			t.Errorf("didn't expect errors pinging the database")
		}

	})
}

func TestWeOSApp_AddProjection(t *testing.T) {
	t.Skip("use of application object is deprecated")
	config := &weos.ServiceConfig{
		ModuleID:  "1iPwGftUqaP4rkWdvFp6BBW2tOf",
		Title:     "Test Module",
		AccountID: "1iPwIGTgWVGyl4XfgrhCqYiiQ7d",
		Database: &weos.DBConfig{
			Driver:   "ramsql",
			Host:     "localhost",
			User:     "root",
			Password: "password",
			Port:     5432,
			Database: "test",
		},
		Log: &weos.LogConfig{
			Level:        "debug",
			ReportCaller: false,
			Formatter:    "text",
		},
	}
	mockProjection := &GormProjectionMock{
		GetEventHandlerFunc: func() weos.EventHandler {
			return func(ctx context.Context, event weos.Event) error {
				return nil
			}
		},
		MigrateFunc: func(ctx context.Context, builders map[string]dynamicstruct.Builder, deletedFields map[string][]string) error {
			return nil
		},
	}
	mockEventRepository := &EventRepositoryMock{
		MigrateFunc: func(ctx context.Context) error {
			return nil
		},
		AddSubscriberFunc: func(handler weos.EventHandler) {

		},
	}
	app, err := weos.NewApplicationFromConfig(config, nil, nil, nil, mockEventRepository)
	if err != nil {
		t.Fatalf("unexpected error occured setting up module '%s'", err)
	}

	err = app.AddProjection(mockProjection)
	if err != nil {
		t.Fatalf("unexpected error occured setting up projection '%s'", err)
	}

	err = app.Migrate(context.TODO(), nil)
	if err != nil {
		t.Fatalf("unexpected error running migrations '%s'", err)
	}

	if len(mockProjection.MigrateCalls()) != 1 {
		t.Errorf("expected the migrate function to be called %d time, called %d times", 1, len(mockProjection.MigrateCalls()))
	}

	if len(mockProjection.GetEventHandlerCalls()) != 1 {
		t.Errorf("expected the get event handler to be called %d time, called %d times", 1, len(mockProjection.GetEventHandlerCalls()))
	}

	//TODO confirm that the handler from the projection is added to the event repository IF one is configured
}
