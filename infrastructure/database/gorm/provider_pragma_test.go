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
	"strings"
	"testing"
)

func TestSqliteDSNWithWorkerPragmas(t *testing.T) {
	t.Parallel()

	t.Run("adds all pragmas to a bare file DSN", func(t *testing.T) {
		got := sqliteDSNWithWorkerPragmas("weos.db")
		for _, want := range []string{"_journal_mode=WAL", "_busy_timeout=5000", "_txlock=immediate"} {
			if !strings.Contains(got, want) {
				t.Errorf("expected %q in %q", want, got)
			}
		}
		if strings.Count(got, "?") != 1 {
			t.Errorf("expected exactly one '?' separator, got %q", got)
		}
	})

	t.Run("preserves existing query params and caller pragmas", func(t *testing.T) {
		got := sqliteDSNWithWorkerPragmas("file:weos.db?_foreign_keys=1&_journal_mode=DELETE")
		// Caller's journal mode must be respected (not overridden to WAL).
		if strings.Contains(got, "_journal_mode=WAL") {
			t.Errorf("should not override caller's _journal_mode, got %q", got)
		}
		if !strings.Contains(got, "_foreign_keys=1") {
			t.Errorf("should preserve existing params, got %q", got)
		}
		// The missing pragmas are still appended with '&'.
		if !strings.Contains(got, "_busy_timeout=5000") || !strings.Contains(got, "_txlock=immediate") {
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
