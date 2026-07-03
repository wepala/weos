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
	"log"
	"os"
	"strings"
	"time"

	weosmodels "github.com/wepala/weos/v3/infrastructure/models"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/oauth"

	authgorm "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/database/gorm"
	// The pure-Go SQLite driver (no cgo) so cross-compiled builds work and
	// FTS5 is unconditionally available — the cgo driver only ships FTS5
	// behind the sqlite_fts5 build tag.
	"github.com/glebarez/sqlite"
	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// gormConfig returns the shared GORM config. The logger writes to stderr, never
// stdout: the `weos mcp` stdio transport speaks JSON-RPC over stdout, and any
// stray write there corrupts the protocol stream. (GORM's default logger logs
// to stdout — which silently breaks stdio MCP clients.) RecordNotFound is
// ignored because the repositories map it to a domain not-found error as normal
// control flow, so it is not worth logging.
func gormConfig() *gorm.Config {
	return &gorm.Config{
		Logger: gormlogger.New(
			// No "\r\n" prefix (GORM's default uses one, which prepends a blank
			// line to every entry) — keep stderr log lines clean.
			log.New(os.Stderr, "", log.LstdFlags),
			gormlogger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  gormlogger.Warn,
				IgnoreRecordNotFoundError: true,
				Colorful:                  false,
			},
		),
	}
}

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
	db, err := gorm.Open(DialectorForDSN(dsn), gormConfig())
	if err != nil {
		return GormDBResult{}, fmt.Errorf("failed to connect to database: %w", err)
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
		&weosmodels.EventReference{},
		&weosmodels.ResourcePermission{},
		&weosmodels.BehaviorSettings{},
		&oauth.OAuthClient{},
		&oauth.OAuthAuthorizationCode{},
		&oauth.OAuthRefreshToken{},
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

// DialectorForDSN detects the database driver from the DSN the same way the
// main DB provider does: PostgreSQL DSNs start with "host=" or contain a
// postgres:// URI; everything else is treated as SQLite (file path or file:
// URI, with the worker pragmas applied). Consumers that need their own GORM
// connection to the configured database (e.g. the ADK session service) use
// this so driver selection never diverges.
func DialectorForDSN(dsn string) gorm.Dialector {
	if strings.HasPrefix(dsn, "host=") || strings.Contains(dsn, "postgres://") || strings.Contains(dsn, "postgresql://") {
		return postgres.Open(dsn)
	}
	return sqlite.Open(sqliteDSNWithWorkerPragmas(dsn))
}

// sqliteDSNWithWorkerPragmas augments a file-based SQLite DSN with the pragmas
// the background subscriber runtime needs to coexist with the synchronous write
// path. Background workers add concurrent writers (batch transactions plus the
// request-path appends), and SQLite allows only one writer at a time:
//
//   - journal_mode(WAL) lets the workers' feed reads run concurrently with a
//     write transaction instead of blocking on it.
//   - busy_timeout makes a writer wait for the lock instead of failing
//     immediately with "database is locked".
//   - _txlock=immediate takes the write lock at BEGIN so concurrent writers
//     serialize cleanly via busy_timeout rather than erroring on a deferred
//     lock upgrade mid-transaction.
//
// Pragmas use the glebarez/modernc DSN form — repeated
// `_pragma=name(value)` query parameters — not the cgo driver's `_name=value`
// form. In-memory databases (tests) are left untouched — WAL is not meaningful
// there and they are single-connection. Any pragma the caller already set in
// the DSN is preserved.
func sqliteDSNWithWorkerPragmas(dsn string) string {
	if strings.Contains(dsn, ":memory:") || strings.Contains(dsn, "mode=memory") {
		return dsn
	}
	pragmas := []struct{ key, param string }{
		{"journal_mode", "_pragma=journal_mode(WAL)"},
		{"busy_timeout", "_pragma=busy_timeout(5000)"},
		{"_txlock", "_txlock=immediate"},
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	for _, p := range pragmas {
		if strings.Contains(dsn, p.key) {
			continue
		}
		dsn += sep + p.param
		sep = "&"
	}
	return dsn
}
