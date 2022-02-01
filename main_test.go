package main_test

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/gommon/log"
	"github.com/ory/dockertest/v3"
	"github.com/wepala/weos/projections/dialects"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var driver = flag.String("driver", "postgres", "run database tests")

type dbConfig struct {
	Host     string `json:"host"`
	User     string `json:"username"`
	Password string `json:"password"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Driver   string `json:"driver"`
	MaxOpen  int    `json:"max-open"`
	MaxIdle  int    `json:"max-idle"`
}

func TestMain(t *testing.M) {
	flag.Parse()

	var pool *dockertest.Pool
	var resource *dockertest.Resource
	var err error

	dbconfig = dbConfig{
		Host:     "localhost",
		User:     "root",
		Password: "secret",
		Port:     49179,
		Database: "",
		Driver:   *driver,
		MaxOpen:  4,
		MaxIdle:  1,
	}
	switch *driver {
	case "postgres":
		log.Info("Started postgres database")
		// uses a sensible default on windows (tcp/http) and linux/osx (socket)
		pool, err = dockertest.NewPool("")
		if err != nil {
			log.Fatalf("Could not connect to docker: %s", err.Error())
		}

		// pulls an image, creates a container based on it and runs it
		resource, err = pool.Run("postgres", "10.7", []string{"POSTGRES_USER=root", "POSTGRES_PASSWORD=secret", "POSTGRES_DB=test"})
		if err != nil {
			log.Fatalf("Could not start resource: %s", err.Error())
		}

		// exponential backoff-retry, because the module in the container might not be ready to accept connections yet
		if err = pool.Retry(func() error {
			db, err = sql.Open("postgres", fmt.Sprintf("host=localhost port=%s user=root password=secret sslmode=disable database=test", resource.GetPort("5432/tcp")))
			//db, err = pgx.Connect(context.Background(),fmt.Sprintf("host=localhost port=%s user=root password=secret sslmode=disable database=test", resource.GetPort("5432/tcp")))
			if err != nil {
				return err
			}
			return db.Ping()
		}); err != nil {
			log.Fatalf("Could not connect to docker: %s", err.Error())
		}

		connection := strings.Split(resource.GetHostPort("5432/tcp"), ":")

		dbconfig.Host = connection[0]
		dbconfig.Port, _ = strconv.Atoi(connection[1])
		gormDB, err = gorm.Open(dialects.NewPostgres(postgres.Config{
			Conn: db,
		}), nil)
		if err != nil {
			log.Fatalf("failed to create postgres database '%s'", err)
		}
	case "mysql":
		log.Info("Started mysql database")
		// uses a sensible default on windows (tcp/http) and linux/osx (socket)
		pool, err = dockertest.NewPool("")
		if err != nil {
			log.Fatalf("Could not connect to docker: %s", err)
		}

		// pulls an image, creates a container based on it and runs it
		resource, err = pool.Run("mysql", "5.7", []string{"MYSQL_ROOT_PASSWORD=secret"})
		if err != nil {
			log.Fatalf("Could not start resource: %s", err)
		}

		// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
		if err = pool.Retry(func() error {
			db, err = sql.Open("mysql", fmt.Sprintf("root:secret@(localhost:%s)/mysql?sql_mode='ERROR_FOR_DIVISION_BY_ZERO,STRICT_TRANS_TABLES,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION'&parseTime=%s", resource.GetPort("3306/tcp"), strconv.FormatBool(true)))
			if err != nil {
				return err
			}
			return db.Ping()
		}); err != nil {
			log.Fatalf("Could not connect to docker: %s", err)
		}
		connection := strings.Split(resource.GetHostPort("3306/tcp"), ":")
		dbconfig.Host = connection[0]
		dbconfig.Port, _ = strconv.Atoi(connection[1])
		gormDB, err = gorm.Open(dialects.NewMySQL(mysql.Config{
			Conn: db,
		}), nil)
		if err != nil {
			log.Fatalf("failed to create mysql database '%s'", err)
		}
	case "sqlite3":
		log.Infof("Started sqlite3 database")
		db, err = sql.Open(*driver, "e2e.db")
		if err != nil {
			log.Fatalf("failed to create sqlite database '%s'", err)
		}
		db.Exec("PRAGMA foreign_keys = ON")
		gormDB, err = gorm.Open(&dialects.SQLite{
			sqlite.Dialector{
				Conn: db,
			},
		}, nil)
		if err != nil {
			log.Fatalf("failed to create sqlite database '%s'", err)
		}
	}
	defer db.Close()
	code := t.Run()

	switch *driver {
	case "postgres", "mysql":
		// You can't defer this because os.Exit doesn't care for defer
		err = pool.Purge(resource)
		if err != nil {
			log.Fatalf("Could not purge resource: %s", err.Error())
		}
	}

	os.Remove("./e2e.db")
	os.Exit(code)
}
