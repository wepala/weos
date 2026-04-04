// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package gorm

import (
	"database/sql"
	"fmt"
	"strings"

	weosmodels "weos/infrastructure/models"
	"weos/internal/config"

	authgorm "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/database/gorm"
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

	models := []any{
		&weosmodels.ResourceType{},
		&weosmodels.Resource{},
		&weosmodels.SidebarSettings{},
		&weosmodels.RoleSettings{},
		&weosmodels.RoleResourceAccess{},
		&weosmodels.Triple{},
		&weosmodels.ResourcePermission{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		return GormDBResult{}, fmt.Errorf("failed to run auto migrate: %w", err)
	}
	if err := authgorm.AutoMigrate(db); err != nil {
		return GormDBResult{}, fmt.Errorf("failed to run auth auto migrate: %w", err)
	}

	return GormDBResult{
		GormDB: db,
		SQLDB:  sqlDB,
	}, nil
}
