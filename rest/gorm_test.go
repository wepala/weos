package rest_test

import (
	"database/sql"
	"github.com/labstack/gommon/log"
	"github.com/wepala/weos/v2/rest"
	"golang.org/x/net/context"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestGORMProjection_Persist(t *testing.T) {
	logger := &LogMock{
		DebugfFunc: func(format string, args ...interface{}) {

		},
		DebugFunc: func(args ...interface{}) {

		},
		ErrorfFunc: func(format string, args ...interface{}) {

		},
		ErrorFunc: func(args ...interface{}) {

		},
	}
	log.Infof("Started sqlite3 database")
	db, err := sql.Open("sqlite3", "projection.db")
	if err != nil {
		log.Fatalf("failed to create sqlite database '%s'", err)
	}
	gormDB, err := gorm.Open(
		sqlite.Dialector{
			Conn: db,
		}, nil)
	if err != nil {
		log.Fatalf("failed to create sqlite database '%s'", err)
	}
	t.Run("persist events", func(t *testing.T) {
		params := rest.GORMProjectionParams{
			GORMDB:       gormDB,
			EventConfigs: nil,
		}
		result, err := rest.NewGORMProjection(params)
		if err != nil {
			t.Fatalf("unexpected error loading api '%s'", err)
		}
		gormEventDispatcher := result.Dispatcher
		var events []rest.Resource
		event := &rest.Event{
			Type: "create",
			Meta: rest.EventMeta{
				ResourceID:    "/blog/test",
				ResourceType:  "http://schema.org/Blog",
				SequenceNo:    1,
				User:          "",
				ApplicationID: "",
				RootID:        "",
				AccountID:     "",
				Created:       "",
			},
		}
		events = append(events, event)
		errs := gormEventDispatcher.Persist(context.TODO(), logger, events)
		if len(errs) > 0 {
			t.Errorf("expected no error, got %v", errs)
		}
	})
}
