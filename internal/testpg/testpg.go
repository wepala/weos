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

// Package testpg gives a test its own empty PostgreSQL database, created on
// the server a TEST_POSTGRES_DSN names and dropped when the test ends. Tests
// that read whole tables (a search index, a truncate) cannot share the one
// database the DSN names without seeing each other's rows.
package testpg

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TB is the part of testing.TB this package needs; godog worlds pass an
// adapter.
type TB interface {
	Helper()
	Cleanup(func())
}

// seq keeps two databases created in the same clock tick apart.
var seq atomic.Int64

var dbNameParam = regexp.MustCompile(`(^|\s)dbname=\S+`)

// WithDatabase returns dsn with its database replaced by name. It accepts
// both DSN forms PostgreSQL drivers take: a postgres:// URL and key=value.
func WithDatabase(dsn, name string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("parse DSN: %w", err)
		}
		u.Path = "/" + name
		return u.String(), nil
	}
	if dbNameParam.MatchString(dsn) {
		return dbNameParam.ReplaceAllString(dsn, "${1}dbname="+name), nil
	}
	return strings.TrimSpace(dsn) + " dbname=" + name, nil
}

// FreshDatabase creates an empty database on adminDSN's server and returns a
// DSN for it. The database is dropped (forcing out any connection still open)
// when the test ends; a failed drop leaves a uniquely named database behind
// and is otherwise harmless.
func FreshDatabase(t TB, adminDSN, prefix string) (string, error) {
	t.Helper()
	admin, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return "", fmt.Errorf("open admin connection: %w", err)
	}
	sqlDB, err := admin.DB()
	if err != nil {
		return "", fmt.Errorf("admin connection pool: %w", err)
	}
	name := fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), seq.Add(1))
	// The name is generated here from a fixed prefix and a number, never
	// from input, so it is safe to interpolate into DDL.
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		// Best-effort: the CREATE DATABASE error is the one to report; a
		// failed close of the admin pool loses nothing a test reads.
		_ = sqlDB.Close()
		return "", fmt.Errorf("create database %s: %w", name, err)
	}
	t.Cleanup(func() {
		// Best-effort: a database left behind is uniquely named and empty
		// of anything another test reads.
		_ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)").Error
		_ = sqlDB.Close()
	})
	return WithDatabase(adminDSN, name)
}
