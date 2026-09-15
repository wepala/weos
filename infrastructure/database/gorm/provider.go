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

	// Configure the connection pool per dialect. PostgreSQL has real MVCC
	// concurrency and keeps its large pool. SQLite keeps a small pool for
	// concurrent readers (WAL); writers are serialized by the write gate (see
	// sqlite_write_gate.go), not by pool size — the pool must stay above one
	// connection, because a single shared connection forces reads and boot to
	// queue behind background subscriber writes (the regression that sank the
	// unmerged #425).
	if params.Config.IsPostgres() {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)
	} else {
		sqlDB.SetMaxIdleConns(5)
		sqlDB.SetMaxOpenConns(10)
	}

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
		&weosmodels.FeatureSetting{},
		&weosmodels.FeatureGrant{},
		&weosmodels.AccountErasure{},
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
// main DB provider does: PostgreSQL DSNs (per config.IsPostgresDSN) get the
// postgres driver untouched; everything else is treated as SQLite (file path
// or file: URI, with the worker pragmas applied and the write gate installed).
// Consumers that need their own GORM connection to the configured database
// (e.g. the ADK session service) use this so driver selection — and write
// serialization — never diverges.
func DialectorForDSN(dsn string) gorm.Dialector {
	if config.IsPostgresDSN(dsn) {
		return postgres.Open(dsn)
	}
	augmented := sqliteDSNWithWorkerPragmas(dsn)
	if strings.Contains(dsn, ":memory:") || strings.Contains(dsn, "mode=memory") {
		// In-memory databases are single-connection and unique per DSN;
		// keying a shared gate on ":memory:" would serialize unrelated test
		// databases against each other. Skip the gate, same as the pragmas.
		return sqlite.Open(augmented)
	}
	return newGatedSQLiteDialector(augmented, dsn)
}

// ReadOnlyDialectorForDSN detects the driver the way DialectorForDSN does, for a
// command that only reads. PostgreSQL DSNs get the postgres driver untouched.
// A SQLite DSN is opened read-only (mode=ro) with none of the worker pragmas and
// no write gate: the server's journal_mode(WAL) pragma rewrites the header of a
// file in rollback journal mode, and a read-only connection refuses it.
func ReadOnlyDialectorForDSN(dsn string) gorm.Dialector {
	if config.IsPostgresDSN(dsn) {
		return postgres.Open(dsn)
	}
	return sqlite.Open(sqliteReadOnlyDSN(dsn))
}

// IsSQLiteMemoryDSN reports whether SQLite opens dsn as an in-memory database:
// the name is exactly ":memory:", plain or as the path of a file: URI, or dsn
// is a file: URI whose query sets mode=memory. The driver cuts the query off a
// plain path before it opens the file, so a mode parameter there names nothing.
// A file whose name only contains either text is a file. A file: URI's fragment
// names nothing, so it is ignored.
func IsSQLiteMemoryDSN(dsn string) bool {
	uri, isURI := strings.CutPrefix(dsn, "file:")
	if !isURI {
		name, _, _ := strings.Cut(dsn, "?")
		return name == ":memory:"
	}
	uri, _, _ = strings.Cut(uri, "#")
	path, query, _ := strings.Cut(uri, "?")
	if path == ":memory:" {
		return true
	}
	for _, param := range strings.Split(query, "&") {
		key, value, _ := strings.Cut(param, "=")
		if sqliteURIUnescape(key) == "mode" && sqliteURIUnescape(value) == "memory" {
			return true
		}
	}
	return false
}

// SQLiteFileName is the file a SQLite DSN names, as the driver and SQLite read
// it. A plain path's query is cut off. A file: URI's path ends at its query or
// fragment, and its %HH escapes are decoded, so file:/data/my%20db.sqlite names
// "/data/my db.sqlite".
func SQLiteFileName(dsn string) string {
	name, _, _ := strings.Cut(dsn, "?")
	path, isURI := strings.CutPrefix(name, "file:")
	if !isURI {
		return name
	}
	path, _, _ = strings.Cut(path, "#")
	return sqliteURIUnescape(path)
}

// sqliteURIUnescape decodes the %HH escapes in part of a SQLite URI the way
// SQLite does: a % not followed by two hex digits is kept as it is.
func sqliteURIUnescape(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHexDigit(s[i+1]) && isHexDigit(s[i+2]) {
			b.WriteByte(hexValue(s[i+1])<<4 | hexValue(s[i+2]))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isHexDigit(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

func hexValue(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	default:
		return c - '0'
	}
}

// sqliteURIPathEscaper escapes the characters a SQLite URI path gives meaning to.
var sqliteURIPathEscaper = strings.NewReplacer("%", "%25", "#", "%23")

// sqliteReadOnlyDSN rewrites a file-based SQLite DSN as a read-only file: URI.
// The driver reads URI parameters such as mode only from a file: URI, so a
// plain path is rewritten as one. A mode, a _txlock or a journal_mode pragma
// the DSN already names is dropped; every other parameter is kept. A file: URI's
// fragment is dropped too: SQLite ignores everything after the #, so a mode
// written after it would not apply. In-memory databases are left untouched.
func sqliteReadOnlyDSN(dsn string) string {
	if IsSQLiteMemoryDSN(dsn) {
		return dsn
	}
	if strings.HasPrefix(dsn, "file:") {
		dsn, _, _ = strings.Cut(dsn, "#")
	}
	name, query, _ := strings.Cut(dsn, "?")
	if !strings.HasPrefix(name, "file:") {
		name = "file:" + sqliteURIPathEscaper.Replace(name)
	}
	var params []string
	for _, param := range strings.Split(query, "&") {
		key, value, _ := strings.Cut(param, "=")
		switch {
		case param == "", key == "mode", key == "_txlock":
			continue
		case key == "_pragma" && strings.HasPrefix(strings.ToLower(value), "journal_mode"):
			continue
		}
		params = append(params, param)
	}
	params = append(params, "mode=ro")
	return name + "?" + strings.Join(params, "&")
}

// sqliteDSNWithWorkerPragmas augments a file-based SQLite DSN with the pragmas
// the background subscriber runtime needs to coexist with the synchronous write
// path. Background workers add concurrent writers (batch transactions plus the
// request-path appends), and SQLite allows only one writer at a time:
//
//   - journal_mode(WAL) lets the workers' feed reads run concurrently with a
//     write transaction instead of blocking on it.
//   - busy_timeout makes a writer wait for the lock instead of failing
//     immediately with "database is locked". It is generous (15s) because a
//     background subscriber's write can queue behind another subscriber
//     whose handler is LLM-latency-bound (memory consolidation extracts
//     facts via a model call between reads and its write) — 5s left the
//     checkpoint write timing out under that load.
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
		{"busy_timeout", "_pragma=busy_timeout(15000)"},
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
