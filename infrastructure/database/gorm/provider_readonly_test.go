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
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// wm-govvg. A read-only command opens a SQLite file as a read-only file: URI,
// because the driver reads mode only from a file: URI, and with no parameter
// that writes: no journal_mode pragma, no _txlock, and no mode but ro. Every
// other parameter the DSN names is kept, and an in-memory DSN is untouched.
//
// Copilot review 5205110975: in-memory means what SQLite takes it to mean — the
// name ":memory:", or a mode=memory parameter on a file: URI (the driver cuts
// the query off a plain path). A file whose name only contains that text is a
// file, and is opened read-only.
func TestSQLiteReadOnlyDSN(t *testing.T) {
	for _, tc := range []struct{ dsn, want string }{
		{"weos.db", "file:weos.db?mode=ro"},
		{"/var/lib/weos/weos.db", "file:/var/lib/weos/weos.db?mode=ro"},
		{"/data/100%#1.db", "file:/data/100%25%231.db?mode=ro"},
		{"file:/data/weos.db?mode=ro", "file:/data/weos.db?mode=ro"},
		{"file:/data/weos.db?mode=rwc&_pragma=foreign_keys(1)", "file:/data/weos.db?_pragma=foreign_keys(1)&mode=ro"},
		{
			"/data/weos.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_txlock=immediate",
			"file:/data/weos.db?_pragma=busy_timeout(15000)&mode=ro",
		},
		{"file:/data/weos.db?_pragma=JOURNAL_MODE%28WAL%29", "file:/data/weos.db?mode=ro"},
		{":memory:", ":memory:"},
		{"file:audit?mode=memory&cache=shared", "file:audit?mode=memory&cache=shared"},
		{"file::memory:?cache=shared", "file::memory:?cache=shared"},
		{":memory:?_pragma=foreign_keys(1)", ":memory:?_pragma=foreign_keys(1)"},
		{"file:/data/weos.db?cache=shared&mode=memory", "file:/data/weos.db?cache=shared&mode=memory"},
		{"/var/lib/weos/mode=memory.db", "file:/var/lib/weos/mode=memory.db?mode=ro"},
		{"file:/var/lib/weos/mode=memory.db?cache=shared", "file:/var/lib/weos/mode=memory.db?cache=shared&mode=ro"},
		{"/data/:memory:.db", "file:/data/:memory:.db?mode=ro"},
		{"weos.db?mode=memory", "file:weos.db?mode=ro"},
		{"file:/var/lib/weos/weos.db#top", "file:/var/lib/weos/weos.db?mode=ro"},
		{"file:/var/lib/weos/weos.db?cache=shared#top", "file:/var/lib/weos/weos.db?cache=shared&mode=ro"},
		{"file::memory:#top", "file::memory:#top"},
	} {
		if got := sqliteReadOnlyDSN(tc.dsn); got != tc.want {
			t.Errorf("sqliteReadOnlyDSN(%q) = %q, want %q", tc.dsn, got, tc.want)
		}
	}
}

// wm-govvg, Copilot review 5205459650. SQLite ignores everything after a file:
// URI's #, so a mode=ro written after a fragment would open the file writable.
// A file: DSN with a fragment is opened read-only, and a write through it fails.
func TestReadOnlyDialectorRefusesAWriteThroughAFileURIWithAFragment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "weos.db")
	writable, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := writable.Exec("CREATE TABLE notes (body TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if db, err := writable.DB(); err == nil {
		_ = db.Close()
	}

	for _, dsn := range []string{"file:" + path + "#top", "file:" + path + "?cache=shared#top"} {
		db, err := gorm.Open(ReadOnlyDialectorForDSN(dsn), &gorm.Config{})
		if err != nil {
			t.Fatalf("opening %q read-only: %v", dsn, err)
		}
		if err := db.Exec("INSERT INTO notes (body) VALUES ('written')").Error; err == nil {
			t.Errorf("a write through the read-only DSN of %q succeeded, want it refused", dsn)
		}
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
}
