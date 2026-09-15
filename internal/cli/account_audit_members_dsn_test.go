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

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// wm-govvg, Copilot review 5205110975. The audit's preflight names a SQLite
// file that does not exist, so the command does not open the path and leave an
// empty database behind. An in-memory database is never missing, and it is
// recognized the way the read-only dialector recognizes it: a file whose name
// only contains ":memory:" or "mode=memory" is a file, and can be missing.
func TestAuditSQLiteFileMissing(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"my db.sqlite", "100%20.db"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	for _, tc := range []struct {
		name        string
		dsn         string
		wantPath    string
		wantMissing bool
	}{
		{"an in-memory name", ":memory:", "", false},
		{"an in-memory file URI", "file::memory:?cache=shared", "", false},
		{"a mode=memory file URI", "file:audit?mode=memory&cache=shared", "", false},
		{"a Postgres DSN", "postgres://weos@localhost/weos", "", false},
		{
			"a missing file named mode=memory",
			filepath.Join(dir, "mode=memory.db"),
			filepath.Join(dir, "mode=memory.db"), true,
		},
		{
			"a missing file URI named mode=memory",
			"file:" + filepath.Join(dir, "mode=memory.db") + "?cache=shared",
			filepath.Join(dir, "mode=memory.db"), true,
		},
		{
			"a missing file named :memory:",
			filepath.Join(dir, ":memory:.db"),
			filepath.Join(dir, ":memory:.db"), true,
		},
		// SQLite decodes %HH in a file: URI's path, so the file the preflight
		// looks for is the decoded one.
		{"an existing file URI with an encoded space", "file:" + dir + "/my%20db.sqlite?mode=ro", "", false},
		{
			"a missing file URI with an encoded space",
			"file:" + dir + "/gone%20db.sqlite",
			filepath.Join(dir, "gone db.sqlite"), true,
		},
		{"an existing plain path with a literal %20", filepath.Join(dir, "100%20.db"), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, missing := sqliteFileMissing(tc.dsn)
			if path != tc.wantPath || missing != tc.wantMissing {
				t.Errorf("sqliteFileMissing(%q) = %q, %v; want %q, %v", tc.dsn, path, missing, tc.wantPath, tc.wantMissing)
			}
		})
	}
}
