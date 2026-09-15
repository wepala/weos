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

import "testing"

// wm-govvg. A read-only command opens a SQLite file as a read-only file: URI,
// because the driver reads mode only from a file: URI, and with no parameter
// that writes: no journal_mode pragma, no _txlock, and no mode but ro. Every
// other parameter the DSN names is kept, and an in-memory DSN is untouched.
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
	} {
		if got := sqliteReadOnlyDSN(tc.dsn); got != tc.want {
			t.Errorf("sqliteReadOnlyDSN(%q) = %q, want %q", tc.dsn, got, tc.want)
		}
	}
}
