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
	"path/filepath"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/internal/config"
	"go.uber.org/fx"
)

func TestSqliteDSNWithWorkerPragmas(t *testing.T) {
	t.Parallel()

	t.Run("adds all pragmas to a bare file DSN", func(t *testing.T) {
		got := sqliteDSNWithWorkerPragmas("weos.db")
		for _, want := range []string{
			"_pragma=journal_mode(WAL)", "_pragma=busy_timeout(15000)", "_txlock=immediate",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("expected %q in %q", want, got)
			}
		}
		if strings.Count(got, "?") != 1 {
			t.Errorf("expected exactly one '?' separator, got %q", got)
		}
	})

	t.Run("preserves existing query params and caller pragmas", func(t *testing.T) {
		got := sqliteDSNWithWorkerPragmas(
			"file:weos.db?_pragma=foreign_keys(1)&_pragma=journal_mode(DELETE)")
		// Caller's journal mode must be respected (not overridden to WAL).
		if strings.Contains(got, "journal_mode(WAL)") {
			t.Errorf("should not override caller's journal_mode, got %q", got)
		}
		if !strings.Contains(got, "_pragma=foreign_keys(1)") {
			t.Errorf("should preserve existing params, got %q", got)
		}
		// The missing pragmas are still appended with '&'.
		if !strings.Contains(got, "_pragma=busy_timeout(15000)") ||
			!strings.Contains(got, "_txlock=immediate") {
			t.Errorf("expected missing pragmas appended, got %q", got)
		}
	})

	t.Run("leaves in-memory DSNs untouched", func(t *testing.T) {
		for _, dsn := range []string{":memory:", "file::memory:?cache=shared", "file:test?mode=memory"} {
			if got := sqliteDSNWithWorkerPragmas(dsn); got != dsn {
				t.Errorf("in-memory DSN should be unchanged: %q -> %q", dsn, got)
			}
		}
	})
}

func TestIsPostgresDSN(t *testing.T) {
	postgres := []string{
		"host=localhost user=x dbname=y",
		"postgres://u:p@localhost:5432/db",
		"postgresql://u:p@localhost/db",
	}
	for _, dsn := range postgres {
		if !isPostgresDSN(dsn) {
			t.Errorf("%q should be detected as PostgreSQL", dsn)
		}
	}
	sqlite := []string{"/tmp/app.db", "file:app.db?cache=shared", ":memory:", "app.db"}
	for _, dsn := range sqlite {
		if isPostgresDSN(dsn) {
			t.Errorf("%q should be treated as SQLite", dsn)
		}
	}
}

func TestProvideGormDB_SqlitePoolIsSingleConnection(t *testing.T) {
	// SQLite has one writer; the pool must be pinned to a single connection
	// so subscriber checkpoint writes and request writes serialize instead of
	// colliding on SQLITE_BUSY.
	dsn := filepath.Join(t.TempDir(), "pool.db")
	res, err := ProvideGormDB(struct {
		fx.In
		Config config.Config
	}{Config: config.Config{DatabaseDSN: dsn}})
	if err != nil {
		t.Fatalf("ProvideGormDB: %v", err)
	}
	if got := res.SQLDB.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("SQLite MaxOpenConnections = %d, want 1", got)
	}
}
