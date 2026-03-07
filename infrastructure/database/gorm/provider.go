package gorm

import (
	"database/sql"
	"fmt"
	"strings"

	"weos/internal/config"

	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// GormDBResult holds the GORM database connection results.
type GormDBResult struct {
	fx.Out
	GormDB *gorm.DB
	SQLDB  *sql.DB
}

// ProvideGormDB creates a GORM database connection.
// Automatically detects whether to use SQLite or PostgreSQL based on the DSN format.
// Returns both *gorm.DB and *sql.DB.
func ProvideGormDB(params struct {
	fx.In
	Config config.Config
}) (GormDBResult, error) {
	dsn := params.Config.DatabaseDSN
	var db *gorm.DB
	var err error

	// Detect database type from DSN
	// PostgreSQL DSNs typically start with "host=" or contain "postgres://"
	// SQLite DSNs are file paths or "file:" URIs
	if strings.HasPrefix(dsn, "host=") || strings.Contains(dsn, "postgres://") || strings.Contains(dsn, "postgresql://") {
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			return GormDBResult{}, fmt.Errorf("failed to connect to PostgreSQL database: %w", err)
		}
	} else {
		db, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		if err != nil {
			return GormDBResult{}, fmt.Errorf("failed to connect to SQLite database: %w", err)
		}
	}

	sqlDB, err := db.DB()
	if err != nil {
		return GormDBResult{}, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Configure connection pool (only meaningful for PostgreSQL)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)

	// TODO: Add your GORM models here for AutoMigrate, e.g.:
	// models := []interface{}{
	// 	&YourModel{},
	// }
	// if err := db.AutoMigrate(models...); err != nil {
	// 	return GormDBResult{}, fmt.Errorf("failed to run auto migrate: %w", err)
	// }

	return GormDBResult{
		GormDB: db,
		SQLDB:  sqlDB,
	}, nil
}
